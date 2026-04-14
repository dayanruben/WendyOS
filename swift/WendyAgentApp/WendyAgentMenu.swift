import SwiftUI
import WendyAgent

struct WendyAgentMenu: View {
    let status: WendyAgentStatus
    let onQuit: () -> Void

    var body: some View {
        Group {
            switch self.status {
            case .failed(let message):
                Text(message)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                Divider()
            case .idle, .starting, .running, .stopping, .stopped:
                EmptyView()
            }

            Button("Quit WendyAgent", action: self.onQuit)
                .keyboardShortcut("q")
        }
    }
}

struct WendyAgentStatusItem: View {
    let status: WendyAgentStatus

    var body: some View {
        Image("StatusIcon")
            .overlay(alignment: .topTrailing) {
                if case .failed = self.status {
                    Image(systemName: "exclamationmark.circle.fill")
                        .symbolRenderingMode(.palette)
                        .foregroundStyle(.white, .red)
                        .font(.system(size: 8, weight: .bold))
                }
            }
            .padding(.trailing, self.badgePadding)
            .help("WendyAgent")
    }

    private var badgePadding: CGFloat {
        switch self.status {
        case .failed:
            8
        case .idle, .starting, .running, .stopping, .stopped:
            0
        }
    }
}
