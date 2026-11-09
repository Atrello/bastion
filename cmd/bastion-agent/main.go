// The Bastion agent: one on each server, connecting out to the panel and doing what it asks with Docker.
//
//	bastion-agent join <panel address> <join token> [--insecure]   once, to join the panel
//	bastion-agent run                                                from then on (systemd runs it)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Atrello/bastion/internal/agent"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "join":
		insecure := len(args) == 4 && args[3] == "--insecure"
		if len(args) != 3 && !insecure {
			usage()
		}
		if err := agent.Join(args[1], args[2], insecure); err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}
		fmt.Println("Joined. Start the agent with: systemctl enable --now bastion-agent")
	case "run":
		cfg, err := agent.LoadConfig()
		if err != nil {
			log.Error(err.Error())
			os.Exit(1)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		agent.Run(ctx, cfg, log)
	case "version":
		fmt.Println(agent.Version)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: bastion-agent join <panel address> <join token> [--insecure] | run | version")
	os.Exit(2)
}
