package main

import (
	"context"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"syscall"
	"time"
	"vault-sa-patcher/config"
	"vault-sa-patcher/internal/controller"
	"vault-sa-patcher/internal/http"
	"vault-sa-patcher/internal/k8s"
	"vault-sa-patcher/internal/vault"
)

var (
	buildTime = "now"
	version   = "local_developer"
)

func main() {
	config.GetCfg()
	ctx, cancelFunction := context.WithCancel(context.Background())
	container := dig.New()
	container.Provide(config.GetCfg)                //nolint:errcheck
	container.Provide(k8s.NewKubeService)           //nolint:errcheck
	container.Provide(http.NewWebServer)            //nolint:errcheck
	container.Provide(vault.New)                    //nolint:errcheck
	container.Provide(controller.NewLoopController) //nolint:errcheck

	zap.S().Infof("vault-sa-patcher starting. Version: %s. (BuiltTime: %s)\n", version, buildTime)

	if err := container.Invoke(func(webServer http.WebServer) {
		webServer.Start()
	}); err != nil {
		zap.S().Fatal(err)
	}

	defer func() {
		zap.S().Info("Main Defer: canceling context")
		cancelFunction()
		time.Sleep(time.Second * 5)
	}()

	if err := container.Invoke(func(cfg *config.Config, vault vault.Service) {
		vault.FetchImagePullSecret(ctx)
		go func() {
			ticker := time.NewTicker(time.Second * time.Duration(12*cfg.Interval))
			for {
				select {
				case <-ctx.Done():
					zap.S().Info("finish main context")
					return
				case t := <-ticker.C:
					zap.S().Info("FetchImagePullSecret start")
					vault.FetchImagePullSecret(ctx)
					zap.S().Info("FetchImagePullSecret finish:", time.Since(t))
				}
			}
		}()
	}); err != nil {
		zap.S().Fatal(err)
	}

	if err := container.Invoke(func(ctlList controller.List) {
		for _, ctl := range ctlList.Controllers {
			go ctl.Start(ctx)
		}
	}); err != nil {
		zap.S().Fatal(err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	sigName := <-signals
	zap.S().Infof("Received SIGNAL - %s. Terminating...", sigName)
}
