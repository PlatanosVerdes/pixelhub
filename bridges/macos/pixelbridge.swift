// pixelbridge: the esp32-divoom TCP protocol on a Mac, over the Mac's own Bluetooth.
// Build: swiftc -O pixelbridge.swift -o pixelbridge
// Run:   ./pixelbridge [-listen 127.0.0.1] [-port 7777]
import Foundation
import IOBluetooth
import Network

let opConnect: UInt8 = 0x69
let opRelease: UInt8 = 0x96
let linkDown: [UInt8] = [0x96]
// Some displays only greet on a fresh baseband link; clients wait for this before sending.
let greeting: [UInt8] = [0x01, 0x09, 0x00, 0x04, 0xbd, 0x55, 0x13, 0x01, 0x1e, 0x00, 0x51, 0x01, 0x02]

func log(_ s: String) {
    let ts = ISO8601DateFormatter().string(from: Date())
    FileHandle.standardError.write("\(ts) \(s)\n".data(using: .utf8)!)
}

func hex(_ b: [UInt8]) -> String { b.map { String(format: "%02x", $0) }.joined() }

// IOBluetooth opens fail when called from a GCD block, which is where network callbacks run; a
// run-loop timer works.
func later(_ seconds: TimeInterval, _ block: @escaping () -> Void) {
    RunLoop.main.add(Timer(timeInterval: seconds, repeats: false) { _ in block() }, forMode: .common)
}

let commandNames: [UInt8: String] = [
    0x04: "ack", 0x18: "datetime", 0x2d: "time-format", 0x44: "image", 0x45: "view", 0x46: "state",
    0x49: "animation", 0x74: "brightness", 0xbd: "design",
]

// Frames as "view 010e0045…(18 B)": the command name, then the bytes cut at 32 so animations stay one line.
func describe(_ b: [UInt8]) -> String {
    let cmd = b.count > 3 ? (b[3] == 0x04 && b.count > 4 ? b[4] : b[3]) : 0
    let name = b.count > 3 && b[3] == 0x04 ? "ack " + (commandNames[cmd] ?? String(format: "%02x", cmd)) : commandNames[cmd] ?? String(format: "%02x", cmd)
    let shown = hex(Array(b.prefix(32))) + (b.count > 32 ? "…" : "")
    return "\(name) \(shown) (\(b.count) B)"
}

func address(_ mac: ArraySlice<UInt8>) -> String { mac.map { String(format: "%02x", $0) }.joined(separator: "-") }

final class Bridge: NSObject, IOBluetoothRFCOMMChannelDelegate {
    private var client: NWConnection?
    private var inbox: [UInt8] = []
    private var device: IOBluetoothDevice?
    private var channel: IOBluetoothRFCOMMChannel?
    private var greeted = false
    private var lastFrame = Date.distantPast

    func accept(_ conn: NWConnection) {
        client?.cancel()
        client = conn
        inbox = []
        conn.stateUpdateHandler = { [weak self, weak conn] state in
            if case .failed = state, let conn { self?.drop(conn) }
            if case .cancelled = state, let conn { self?.drop(conn) }
        }
        conn.start(queue: .main)
        receive(conn)
        log("client connected")
    }

    private func drop(_ conn: NWConnection) {
        if client === conn {
            client = nil
            log("client gone")
        }
    }

    private func receive(_ conn: NWConnection) {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 4096) { [weak self] data, _, done, err in
            guard let self, self.client === conn else { return }
            if let data { self.inbox += data; self.parse() }
            if done || err != nil { conn.cancel(); return }
            self.receive(conn)
        }
    }

    private func reply(_ b: [UInt8]) {
        client?.send(content: Data(b), completion: .contentProcessed { _ in })
    }

    private func parse() {
        while let first = inbox.first {
            switch first {
            case opConnect:
                guard inbox.count >= 8 else { return }
                let msg = Array(inbox.prefix(8)); inbox.removeFirst(8)
                later(0) { self.connect(address(msg[1...6]), channel: msg[7]) }
            case opRelease:
                guard inbox.count >= 7 else { return }
                inbox.removeFirst(7)
                later(0) { self.release() }
            case 0x01:
                guard inbox.count >= 3 else { return }
                let total = (Int(inbox[1]) | Int(inbox[2]) << 8) + 4
                guard inbox.count >= total else { return }
                let frame = Array(inbox.prefix(total)); inbox.removeFirst(total)
                forward(frame)
            default:
                log("skipping stray byte \(String(format: "%02x", first))")
                inbox.removeFirst()
            }
        }
    }

    private func forward(_ frame: [UInt8]) {
        guard let channel, channel.isOpen() else { reply(linkDown); return }
        // Same minimum gap between frames as esp32-divoom (TCP_PACING).
        let wait = 0.015 - Date().timeIntervalSince(lastFrame)
        if wait > 0 { Thread.sleep(forTimeInterval: wait) }
        defer { lastFrame = Date() }
        log("tx " + describe(frame))
        let mtu = max(Int(channel.getMTU()), 1)
        var offset = 0
        while offset < frame.count {
            var chunk = Array(frame[offset..<min(offset + mtu, frame.count)])
            if channel.writeSync(&chunk, length: UInt16(chunk.count)) != kIOReturnSuccess {
                log("write failed")
                close()
                reply(linkDown)
                return
            }
            offset += chunk.count
        }
    }

    private func connect(_ addr: String, channel id: UInt8) {
        if let channel, channel.isOpen() { log("already connected"); return }
        guard let dev = IOBluetoothDevice(addressString: addr) else { log("bad address \(addr)"); return }
        device = dev
        greeted = false
        // Displays that are also headsets share the channel with Hands-Free, which the Mac opens on
        // its own; the serial channel only opens on a link without it.
        var wait = 0.0
        if dev.isConnected() {
            log("dropping the existing link")
            dev.closeConnection()
            wait = 4
        }
        later(wait) { self.open(dev, addr, id) }
    }

    // Retries without dropping the link again: every time the display switched itself off, a drop had followed a failed open.
    private func open(_ dev: IOBluetoothDevice, _ addr: String, _ id: UInt8, attempt: Int = 1) {
        log("connecting \(addr) channel \(id), attempt \(attempt)")
        var ch: IOBluetoothRFCOMMChannel?
        let r = dev.openRFCOMMChannelSync(&ch, withChannelID: id, delegate: self)
        guard r == kIOReturnSuccess, let ch else {
            log("open failed: \(r)")
            ch?.setDelegate(nil)
            ch?.close()
            if attempt < 3 {
                later(2) { self.open(dev, addr, id, attempt: attempt + 1) }
            } else {
                reply(linkDown)
            }
            return
        }
        channel = ch
        linkUp(ch)
    }

    private func linkUp(_ ch: IOBluetoothRFCOMMChannel) {
        log("link up, mtu \(ch.getMTU())")
        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
            if self.channel === ch && !self.greeted { self.greeted = true; self.reply(greeting) }
        }
    }

    private func release() {
        log("release")
        close()
        device?.closeConnection()
    }

    private func close() {
        channel?.setDelegate(nil)
        channel?.close()
        channel = nil
    }

    func rfcommChannelData(_ ch: IOBluetoothRFCOMMChannel!, data: UnsafeMutableRawPointer!, length: Int) {
        let b = Array(UnsafeRawBufferPointer(start: data, count: length))
        var rest = b[...]
        while rest.count > 3, rest.first == 0x01 {
            let n = (Int(rest[rest.startIndex + 1]) | Int(rest[rest.startIndex + 2]) << 8) + 4
            log("rx " + describe(Array(rest.prefix(n))))
            rest = rest.dropFirst(n)
        }
        if !rest.isEmpty { log("rx " + hex(Array(rest))) }
        if b.count > 4 && b[3] == 0x04 && b[4] == 0xbd { greeted = true }
        reply(b)
    }

    func rfcommChannelClosed(_ ch: IOBluetoothRFCOMMChannel!) {
        guard channel === ch else { return }
        log("link down")
        channel = nil
        reply(linkDown)
    }
}

var host = "127.0.0.1"
var port: UInt16 = 7777
var args = CommandLine.arguments.dropFirst().makeIterator()
while let a = args.next() {
    switch a {
    case "-listen": host = args.next() ?? host
    case "-port": port = UInt16(args.next() ?? "") ?? port
    default:
        FileHandle.standardError.write("usage: pixelbridge [-listen host] [-port n]\n".data(using: .utf8)!)
        exit(2)
    }
}

let bridge = Bridge()
let params = NWParameters.tcp
params.requiredLocalEndpoint = .hostPort(host: .init(host), port: .init(rawValue: port)!)
let listener = try NWListener(using: params)
listener.newConnectionHandler = { bridge.accept($0) }
listener.stateUpdateHandler = { state in
    if case .failed(let err) = state { log("listen failed: \(err)"); exit(1) }
    if case .ready = state { log("listening on \(host):\(port)") }
}
listener.start(queue: .main)
RunLoop.main.run()
