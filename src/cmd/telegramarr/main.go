package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/akhil-rana/telegramarr/internal/api"
	"github.com/akhil-rana/telegramarr/internal/auth"
	"github.com/akhil-rana/telegramarr/internal/config"
	"github.com/akhil-rana/telegramarr/internal/logging"
)

func main() {
	// Parse flags
	configPath := flag.String("config", "config.yaml", "Path to config.yaml")
	flag.Parse()

	// Initialize logger
	logger, err := logging.NewLogger("info", "json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	// Load configuration
	cfg, err := config.Load(*configPath, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Override port from PORT environment variable if set
	if portStr := os.Getenv("PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.Server.Port = port
		}
	}

	// Ensure data directories exist
	dataDir, err := config.GetDataDir()
	if err != nil {
		logger.Fatal(err.Error())
	}
	if _, err := config.GetTempDir(); err != nil {
		logger.Fatal(err.Error())
	}

	// Get session file paths
	sessionPath, err := config.SessionFilePath()
	if err != nil {
		logger.Fatal(err.Error())
	}

	_, err = config.SessionDBPath()
	if err != nil {
		logger.Fatal(err.Error())
	}

	// Check if session exists
	var session *auth.SessionData
	if auth.SessionExists(sessionPath) {
		session, err = auth.LoadSession(sessionPath)
		if err != nil {
			logger.Error("Failed to load session: " + err.Error())
			session = nil
		}
	}

	// Print startup info
	border := "=================================================="
	fmt.Println("\n" + border)
	fmt.Println("     Telegramarr - Starting")
	fmt.Println(border)
	fmt.Printf("Config file:   %s\n", *configPath)
	fmt.Printf("Session file:  %s\n", sessionPath)
	fmt.Printf("Data dir:      data/\n")
	fmt.Printf("Temp dir:      temp/\n")
	fmt.Printf("Server port:   %d\n", cfg.Server.Port)

	if session != nil && session.Authenticated {
		fmt.Printf("\n✓ Authenticated as: @%s\n", session.Username)
		fmt.Printf("  User ID: %d\n", session.UserID)
		fmt.Printf("  Radarr Channel: %d\n", cfg.Telegram.RadarrChannelID)
		fmt.Printf("  Sonarr Channel: %d\n", cfg.Telegram.SonarrChannelID)
		fmt.Println("\n✓ Ready for webhooks!")
	} else {
		fmt.Println("\n✗ Not authenticated")
		fmt.Printf("  Please visit http://0.0.0.0:%d to authenticate\n", cfg.Server.Port)
	}

	fmt.Printf("\nVisit: http://localhost:%d\n", cfg.Server.Port)
	fmt.Println(border + "\n")

	// Create and start server
	server := api.NewServer(logger, cfg, session, dataDir)
	if err := server.Run(strconv.Itoa(cfg.Server.Port)); err != nil {
		logger.Fatal(err.Error())
	}
}
