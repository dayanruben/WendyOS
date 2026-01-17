import Foundation
import Logging

#if os(Linux)
import ALSA
#endif

/// Represents an audio device
public struct AudioDevice: Sendable {
    public let id: UInt32
    public let name: String
    public let description: String
    public let type: AudioDeviceType
    public let isDefault: Bool
    public let cardIndex: Int
    public let deviceIndex: Int

    public enum AudioDeviceType: Sendable {
        case input   // Microphone/source
        case output  // Speaker/sink
    }

    /// ALSA device identifier (e.g., "plughw:0,0")
    public var alsaDevice: String {
        "plughw:\(cardIndex),\(deviceIndex)"
    }
}

/// Errors that can occur during audio operations
public enum AudioError: Error, LocalizedError {
    case commandFailed(String)
    case parseError(String)
    case deviceNotFound(UInt32)
    case recordingFailed(String)
    case notAvailable

    public var errorDescription: String? {
        switch self {
        case .commandFailed(let msg): return "Command failed: \(msg)"
        case .parseError(let msg): return "Parse error: \(msg)"
        case .deviceNotFound(let id): return "Device not found: \(id)"
        case .recordingFailed(let msg): return "Recording failed: \(msg)"
        case .notAvailable: return "Audio system not available"
        }
    }
}

// Keep old name for compatibility
public typealias PipeWireError = AudioError

/// Manager for interacting with ALSA audio system
public actor PipeWireManager {
    private let logger = Logger(label: "AudioManager")

    // Track which device is currently selected as "default" (by ID)
    private var defaultInputId: UInt32?
    private var defaultOutputId: UInt32?

    // Cache of devices for quick lookup
    private var cachedDevices: [AudioDevice]?

    public init() {}

    /// List all available audio devices using ALSA
    public func listDevices(typeFilter: AudioDevice.AudioDeviceType? = nil) async throws -> [AudioDevice] {
        logger.debug("Listing audio devices", metadata: ["typeFilter": "\(String(describing: typeFilter))"])

        #if os(Linux)
        guard ALSAAudio.isAvailable else {
            // Include the actual initialization error for debugging
            if let initError = ALSAAudio.initializationError {
                throw AudioError.commandFailed("ALSA not available: \(initError)")
            }
            throw AudioError.notAvailable
        }

        let alsa = try ALSAAudio()
        let alsaDevices = try alsa.listDevices()

        var devices: [AudioDevice] = []
        var nextId: UInt32 = 1

        for alsaDevice in alsaDevices {
            // Add as input device if it supports capture
            if alsaDevice.isCapture && (typeFilter == nil || typeFilter == .input) {
                let isDefault = defaultInputId == nextId || (defaultInputId == nil && nextId == 1)
                devices.append(AudioDevice(
                    id: nextId,
                    name: alsaDevice.name,
                    description: "\(alsaDevice.cardName) - Input",
                    type: .input,
                    isDefault: isDefault,
                    cardIndex: alsaDevice.cardIndex,
                    deviceIndex: alsaDevice.deviceIndex
                ))
                nextId += 1
            }

            // Add as output device if it supports playback
            if alsaDevice.isPlayback && (typeFilter == nil || typeFilter == .output) {
                let firstOutputId = devices.first { $0.type == .output }?.id ?? nextId
                let isDefault = defaultOutputId == nextId || (defaultOutputId == nil && nextId == firstOutputId)
                devices.append(AudioDevice(
                    id: nextId,
                    name: alsaDevice.name,
                    description: "\(alsaDevice.cardName) - Output",
                    type: .output,
                    isDefault: isDefault,
                    cardIndex: alsaDevice.cardIndex,
                    deviceIndex: alsaDevice.deviceIndex
                ))
                nextId += 1
            }
        }

        cachedDevices = devices
        logger.info("Found \(devices.count) audio devices")
        return devices
        #else
        // macOS - return empty for now (audio handled differently)
        return []
        #endif
    }

    /// Set the default audio device (stored in memory for this session)
    public func setDefaultDevice(id: UInt32) async throws {
        logger.info("Setting default device", metadata: ["id": "\(id)"])

        let allDevices = try await listDevices()
        guard let device = allDevices.first(where: { $0.id == id }) else {
            throw AudioError.deviceNotFound(id)
        }

        if device.type == .input {
            defaultInputId = id
        } else {
            defaultOutputId = id
        }

        logger.info("Successfully set default device", metadata: ["id": "\(id)", "type": "\(device.type)"])
    }

    /// Get the ALSA device string for a device ID (or default)
    public func getALSADevice(forId id: UInt32?, type: AudioDevice.AudioDeviceType) async throws -> String {
        let devices = try await listDevices(typeFilter: type)

        if let id = id, id != 0 {
            guard let device = devices.first(where: { $0.id == id }) else {
                throw AudioError.deviceNotFound(id)
            }
            return device.alsaDevice
        }

        // Use default
        let defaultId = type == .input ? defaultInputId : defaultOutputId
        if let defaultId = defaultId, let device = devices.first(where: { $0.id == defaultId }) {
            return device.alsaDevice
        }

        // Fall back to first device
        guard let first = devices.first else {
            throw AudioError.deviceNotFound(0)
        }
        return first.alsaDevice
    }

    /// Stream audio levels from a device using structured concurrency
    ///
    /// - Parameters:
    ///   - deviceId: Device ID to stream from (nil for default)
    ///   - updateRateHz: Update rate in Hz (1-60)
    ///   - handler: Called for each audio level update
    public func withAudioLevels(
        deviceId: UInt32?,
        updateRateHz: UInt32,
        handler: @Sendable @escaping (Float, Float) async throws -> Void
    ) async throws {
        #if os(Linux)
        let alsaDeviceId = try await getALSADevice(forId: deviceId, type: .input)
        let rate = max(1, min(60, updateRateHz))

        logger.debug("Starting audio level monitoring", metadata: ["device": "\(alsaDeviceId)"])

        let alsa = try ALSAAudio()
        let stream = try alsa.openCapture(
            device: alsaDeviceId,
            sampleRate: 48000,
            channels: 1,
            latencyMicroseconds: 50000
        )

        let samplesPerUpdate = Int(48000 / rate)

        while !Task.isCancelled {
            let data = try stream.readData(frameCount: samplesPerUpdate)
            let levels = Self.calculateLevels(from: data)
            try await handler(levels.peakDb, levels.rmsDb)
        }
        #else
        throw AudioError.notAvailable
        #endif
    }

    /// Stream raw PCM audio from a device using structured concurrency
    ///
    /// - Parameters:
    ///   - deviceId: Device ID to stream from (nil for default)
    ///   - sampleRate: Sample rate in Hz
    ///   - channels: Number of channels
    ///   - handler: Called for each audio chunk with (data, timestampNs)
    public func withAudioStream(
        deviceId: UInt32?,
        sampleRate: UInt32,
        channels: UInt32,
        handler: @Sendable @escaping (Data, UInt64) async throws -> Void
    ) async throws {
        #if os(Linux)
        let rate = sampleRate == 0 ? 48000 : sampleRate
        let chans = channels == 0 ? 1 : channels

        let alsaDeviceId = try await getALSADevice(forId: deviceId, type: .input)
        logger.debug(
            "Starting audio stream",
            metadata: [
                "device": "\(alsaDeviceId)",
                "rate": "\(rate)",
                "channels": "\(chans)",
            ]
        )

        let startTime = DispatchTime.now().uptimeNanoseconds

        let alsa = try ALSAAudio()
        let stream = try alsa.openCapture(
            device: alsaDeviceId,
            sampleRate: rate,
            channels: chans,
            latencyMicroseconds: 50000
        )

        // Stream audio in ~20ms chunks
        let framesPerChunk = Int(rate / 50)

        while !Task.isCancelled {
            let data = try stream.readData(frameCount: framesPerChunk)
            let timestampNs = DispatchTime.now().uptimeNanoseconds - startTime
            try await handler(data, timestampNs)
        }
        #else
        throw AudioError.notAvailable
        #endif
    }

    // MARK: - Legacy AsyncThrowingStream API (for gRPC compatibility)

    /// Stream audio levels from a device
    /// Returns an AsyncStream of (peakDb, rmsDb) tuples
    ///
    /// Note: This uses AsyncStream.makeStream() pattern for proper structured concurrency
    public func streamAudioLevels(
        deviceId: UInt32?,
        updateRateHz: UInt32
    ) -> AsyncThrowingStream<(peakDb: Float, rmsDb: Float), Error> {
        let (stream, continuation) = AsyncThrowingStream<(peakDb: Float, rmsDb: Float), Error>.makeStream()

        let task = Task { [self] in
            do {
                try await withAudioLevels(deviceId: deviceId, updateRateHz: updateRateHz) { peakDb, rmsDb in
                    continuation.yield((peakDb: peakDb, rmsDb: rmsDb))
                }
                continuation.finish()
            } catch {
                continuation.finish(throwing: error)
            }
        }

        continuation.onTermination = { _ in
            task.cancel()
        }

        return stream
    }

    /// Stream raw PCM audio from a device
    public func streamAudio(
        deviceId: UInt32?,
        sampleRate: UInt32,
        channels: UInt32
    ) -> AsyncThrowingStream<(data: Data, timestampNs: UInt64), Error> {
        let (stream, continuation) = AsyncThrowingStream<(data: Data, timestampNs: UInt64), Error>.makeStream()

        let task = Task { [self] in
            do {
                try await withAudioStream(deviceId: deviceId, sampleRate: sampleRate, channels: channels) { data, timestampNs in
                    continuation.yield((data: data, timestampNs: timestampNs))
                }
                continuation.finish()
            } catch {
                continuation.finish(throwing: error)
            }
        }

        continuation.onTermination = { _ in
            task.cancel()
        }

        return stream
    }

    // MARK: - Private Helpers

    /// Calculate peak and RMS levels from PCM data
    private static func calculateLevels(from data: Data) -> (peakDb: Float, rmsDb: Float) {
        guard !data.isEmpty else {
            return (peakDb: -96.0, rmsDb: -96.0)
        }

        var peak: Int16 = 0
        var sumSquares: Float = 0

        data.withUnsafeBytes { buffer in
            let samples = buffer.bindMemory(to: Int16.self)
            for sample in samples {
                let absSample = abs(sample)
                if absSample > peak {
                    peak = absSample
                }
                let normalized = Float(sample) / Float(Int16.max)
                sumSquares += normalized * normalized
            }
        }

        let sampleCount = data.count / 2
        let rms = sqrt(sumSquares / Float(max(1, sampleCount)))

        // Convert to dB (0 dB = max amplitude)
        let peakDb = peak > 0 ? 20 * log10(Float(peak) / Float(Int16.max)) : -96.0
        let rmsDb = rms > 0 ? 20 * log10(rms) : -96.0

        return (peakDb: peakDb, rmsDb: rmsDb)
    }
}
