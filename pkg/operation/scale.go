package operation

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/config"
	"github.com/bringg/jenkins-autoscaler/pkg/dispatcher"
	"github.com/bringg/jenkins-autoscaler/pkg/http"
	"github.com/bringg/jenkins-autoscaler/pkg/scaler"
)

const ConfigSectionName = "scaler"

type (
	AutoScaleInput struct {
		BackendName    string
		UseDebugLogger bool

		LogLevel          string `config:"log_level"`
		MetricsServerAddr string `config:"metrics_server_addr"`
	}
)

// AutoScale start the jenkins autoscaler server.
func AutoScale(ctx context.Context, input AutoScaleInput) error {
	info, err := backend.Find(input.BackendName)
	if err != nil {
		return err
	}

	bk, err := info.NewBackend(
		ctx,
		backend.ConfigMap(
			info,
			fmt.Sprintf("backend.%s", info.Name),
		),
	)
	if err != nil {
		return err
	}

	m := backend.ConfigMap(nil, ConfigSectionName)
	if err := config.ReadOptions(m, &input); err != nil {
		return err
	}

	logger, err := createLogger(input.LogLevel, input.UseDebugLogger)
	if err != nil {
		return err
	}

	s, err := scaler.New(m, bk, logger, scaler.NewMetrics())
	if err != nil {
		return err
	}

	disp, err := dispatcher.New(s, m, logger, dispatcher.NewMetrics())
	if err != nil {
		return err
	}

	// start http server for metrics
	srv := http.NewServer(input.MetricsServerAddr)
	done := make(chan error, 1)
	defer close(done)

	go disp.Run(ctx)
	defer disp.Stop()

	go func() {
		if err := srv.Start(); err != nil {
			done <- err
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	sigint := make(chan os.Signal, 1)

	// sigint signal sent from terminal
	// sigterm signal sent from kubernetes
	signal.Notify(sigint, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigint:
		return srv.Shutdown(ctx)
	case err = <-done:
		return err
	}
}

func createLogger(level string, useDebug bool) (*log.Logger, error) {
	logger := log.New()

	lv, err := log.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	if useDebug {
		lv = log.DebugLevel
	}

	logger.SetLevel(lv)

	return logger, nil
}

func (i *AutoScaleInput) Name() string {
	return ConfigSectionName
}
