// pixelbridge lets pixelhub reach a display over this machine's own Bluetooth, speaking the same TCP
// protocol as the esp32-divoom bridge. Linux only; the Mac has its own in bridges/macos.
package main

import (
	"flag"
	"log/slog"
	"net"
	"os"

	"github.com/PlatanosVerdes/pixelhub/internal/btbridge"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:7777", "address pixelhub connects to")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if os.Getenv("PIXELHUB_LOG_JSON") != "" {
		log = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	log.Info("listening", "address", ln.Addr())
	bridge := btbridge.New(dialRFCOMM, log)
	if sp := newSpeaker(log); sp != nil {
		bridge.Chime, bridge.Say = sp.chime(), sp.say
	}
	if err := bridge.Serve(ln); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
