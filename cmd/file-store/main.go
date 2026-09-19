package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	filestore "github.com/kayushkin/file-store"
	"github.com/kayushkin/llm-bridge/servicesettings"
)

func main() {
	definitions, err := filestore.SettingDefinitions()
	if err != nil {
		log.Fatalf("settings: %v", err)
	}
	settings, err := servicesettings.New(filestore.ServiceName, filestore.OwnedEnvironmentPrefixes, definitions, servicesettings.ProcessEnvironment())
	if err != nil {
		log.Fatalf("settings: %v", err)
	}
	if err := settings.CheckRequired(); err != nil {
		log.Fatalf("settings: %v", err)
	}
	settings.SetValidator(filestore.SettingMaximumFileBytes, filestore.ValidateMaximumFileBytes)
	settings.SetValidator(filestore.SettingInlineContentTypes, filestore.ValidateInlineContentTypes)

	store, err := filestore.Open(settings.String(filestore.SettingDataDirectory))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := settings.AttachStoredValues(store.StoredSettings(), nil); err != nil {
		log.Fatalf("settings: %v", err)
	}
	// A value seeded from the environment never passed through Set, so the
	// validators have not seen it yet.
	for key, validate := range map[string]func(string) error{
		filestore.SettingMaximumFileBytes:   filestore.ValidateMaximumFileBytes,
		filestore.SettingInlineContentTypes: filestore.ValidateInlineContentTypes,
	} {
		if err := validate(settings.String(key)); err != nil {
			log.Fatalf("settings: %v", err)
		}
	}

	server, err := filestore.NewServer(store, settings)
	if err != nil {
		log.Fatalf("server: %v", err)
	}
	address := settings.String(filestore.SettingListenAddress)
	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("file-store listening on %s (data=%s); every route but /health needs %s", address, store.DataDir(), filestore.ServiceTokenHeader)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdown); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
