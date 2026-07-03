// ghost.noted , STUB. Binds its loopback health port and reports OK so ghost.secd's supervisor can
// manage it (poll, restart, stop-before-unmount) before the real logic exists. The daemon's actual
// job is described in this directory's README; this binary is the honest placeholder , it does
// nothing but stay alive and answer health, so the supervisor and the app's Ghost Status screen work
// end to end today. Replace the body with real logic behind the same ghosthealth.Reporter contract.
//
// Runs only while the account is UNLOCKED (data lives on the encrypted volume). Exits cleanly on
// SIGTERM so the supervisor's stop-and-confirm-dead teardown never leaves it holding the mount.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/LocalGhostDao/localghost/server/internal/ghosthealth"
)

const service = "ghost.noted"

func main() {
	port := flag.Int("health-port", envPort("GHOST_HEALTH_PORT"), "loopback health/status port (required)")
	flag.Parse()
	if *port <= 0 {
		log.Fatalf("%s: no health port (set --health-port or GHOST_HEALTH_PORT)", service)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := ghosthealth.NewServer(service, ghosthealth.OKReporter{Service: service})
	go func() {
		if err := srv.Serve(*port); err != nil {
			log.Printf("%s: health server stopped: %v", service, err)
		}
	}()
	log.Printf("%s: stub up, health on 127.0.0.1:%d", service, *port)

	<-ctx.Done()
	log.Printf("%s: shutting down", service)
}

func envPort(key string) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}
