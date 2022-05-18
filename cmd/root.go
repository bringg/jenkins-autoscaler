package cmd

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"strings"

	"github.com/rclone/rclone/fs"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/bringg/jenkins-autoscaler/pkg/backend"
	"github.com/bringg/jenkins-autoscaler/pkg/config"
	"github.com/bringg/jenkins-autoscaler/pkg/operation"

	// registry all backends
	_ "github.com/bringg/jenkins-autoscaler/pkg/backend/type/all"
)

var (
	input operation.AutoScaleInput

	configPath = config.GetConfigPath()
	version    = "development"

	rootCmd = &cobra.Command{
		Use:     "jas",
		Short:   "jenkins-autoscaler",
		Version: version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return operation.AutoScale(cmd.Context(), input)
		},
	}
)

func Execute() {
	addConfigFlags()
	addBackendFlags()

	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", configPath, "config file")
	rootCmd.PersistentFlags().StringVarP(&input.BackendName, "backend", "b", input.BackendName, fmt.Sprintf("backend name, one of (%s)", strings.Join(backend.Names(), ", ")))
	rootCmd.PersistentFlags().BoolVarP(&input.UseDebugLogger, "debug", "d", input.UseDebugLogger, "enable debug")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	// Set path to configuration file, overwire default path if config path passed
	if err := config.SetConfigPath(configPath); err != nil {
		log.Fatalf("--config: Failed to set %q as config path: %v", configPath, err)
	}

	cfgFile := config.GetConfigPath()
	viper.SetConfigFile(cfgFile)

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		if errors.Is(err, iofs.ErrNotExist) || errors.As(err, &viper.ConfigFileNotFoundError{}) {
			if cfgFile == "" {
				log.Info("Config is memory-only - using defaults")
			}

			return
		}

		log.WithError(err).Fatalf("Failed to load config file %q", cfgFile)
	}

	log.Infof("Using config file: %q", viper.ConfigFileUsed())
}

// addConfigFlags creates flags for config options
func addConfigFlags() {
	addFlagsFromOptions(config.Options, "scaler")
}

// addBackendFlags creates flags for all the backend options
func addBackendFlags() {
	for _, bkInfo := range backend.Registry {
		addFlagsFromOptions(bkInfo.Options, bkInfo.Name)
	}
}

// part of the logic was used from rclone project
// https://github.com/rclone/rclone/blob/750fffdf71ed51291bd31802c16fc63bd34b1462/cmd/cmd.go#L504
func addFlagsFromOptions(opts fs.Options, prefix string) {
	done := map[string]struct{}{}
	for i := range opts {
		opt := &opts[i]
		// Skip if done already
		if _, doneAlready := done[opt.Name]; doneAlready {
			continue
		}

		done[opt.Name] = struct{}{}
		// Make a flag from each option
		name := opt.FlagName(prefix)
		if pflag.CommandLine.Lookup(name) != nil {
			log.Errorf("Not adding duplicate flag --%s", name)

			continue
		}

		// Take first line of help only
		help := strings.TrimSpace(opt.Help)
		if nl := strings.IndexRune(help, '\n'); nl >= 0 {
			help = help[:nl]
		}

		help = strings.TrimRight(strings.TrimSpace(help), ".!?")
		if opt.IsPassword {
			help += " (obscured)"
		}

		flag := pflag.CommandLine.VarPF(opt, name, opt.ShortOpt, help)
		config.SetDefaultFromEnv(pflag.CommandLine, name)
		if _, isBool := opt.Default.(bool); isBool {
			flag.NoOptDefVal = "true"
		}

		// Hide on the command line if requested
		if opt.Hide&fs.OptionHideCommandLine != 0 {
			flag.Hidden = true
		}
	}
}
