import Foundation

internal struct WendyObservationRegistry<Value: Sendable> {
    internal typealias Observer = @Sendable (Value) -> Void
    internal typealias ObserverID = UUID

    internal struct Delivery: Sendable {
        let observer: Observer
        let value: Value
    }

    init(areEquivalent: @escaping @Sendable (Value, Value) -> Bool) {
        self.areEquivalent = areEquivalent
    }

    mutating func register(_ observer: @escaping Observer, initialValue: Value) -> ObserverID {
        let observerID = ObserverID()
        self.observers[observerID] = .init(observer: observer)
        _ = self.enqueue(initialValue, for: observerID)
        return observerID
    }

    mutating func enqueue(_ value: Value) -> [ObserverID] {
        var observerIDs: [ObserverID] = []
        for observerID in self.observers.keys {
            if self.enqueue(value, for: observerID) {
                observerIDs.append(observerID)
            }
        }
        return observerIDs
    }

    mutating func beginDelivery(for observerID: ObserverID) -> Delivery? {
        guard var observerState = self.observers[observerID] else { return nil }
        guard !observerState.pendingValues.isEmpty else {
            observerState.isDelivering = false
            self.observers[observerID] = observerState
            return nil
        }

        let value = observerState.pendingValues.removeFirst()
        observerState.inFlightValue = value
        self.observers[observerID] = observerState
        return .init(observer: observerState.observer, value: value)
    }

    mutating func finishDelivery(for observerID: ObserverID, delivered value: Value) -> Bool {
        guard var observerState = self.observers[observerID] else { return false }

        observerState.lastDeliveredValue = value
        observerState.inFlightValue = nil

        let shouldContinue = !observerState.pendingValues.isEmpty
        observerState.isDelivering = shouldContinue
        self.observers[observerID] = observerState
        return shouldContinue
    }

    mutating func removeObserver(_ observerID: ObserverID) {
        self.observers.removeValue(forKey: observerID)
    }

    // MARK: - Private

    private struct ObserverState {
        let observer: Observer
        var lastDeliveredValue: Value?
        var inFlightValue: Value?
        var pendingValues: [Value] = []
        var isDelivering = false
    }

    private let areEquivalent: @Sendable (Value, Value) -> Bool
    private var observers: [ObserverID: ObserverState] = [:]

    @discardableResult
    private mutating func enqueue(_ value: Value, for observerID: ObserverID) -> Bool {
        guard var observerState = self.observers[observerID] else { return false }

        if let lastQueuedValue = observerState.pendingValues.last {
            guard !self.areEquivalent(lastQueuedValue, value) else { return false }
        } else if let inFlightValue = observerState.inFlightValue {
            guard !self.areEquivalent(inFlightValue, value) else { return false }
        } else if let lastDeliveredValue = observerState.lastDeliveredValue {
            guard !self.areEquivalent(lastDeliveredValue, value) else { return false }
        }

        observerState.pendingValues.append(value)

        let shouldScheduleDelivery = !observerState.isDelivering
        observerState.isDelivering = true
        self.observers[observerID] = observerState
        return shouldScheduleDelivery
    }
}
