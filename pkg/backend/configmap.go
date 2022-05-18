package backend

import (
	"os"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	log "github.com/sirupsen/logrus"

	"github.com/bringg/jenkins-autoscaler/pkg/config"
)

type (
	// A configmap.Getter to read either the default value or the set
	// value from the fs.Options
	optionsValues struct {
		options    fs.Options
		useDefault bool
	}

	// A configmap.Getter to read from the environment JAS_CONFIG_backend_option_name
	configEnvVars string

	// A configmap.Getter to read from the environment JAS_option_name
	optionEnvVars struct {
		bkInfo *RegInfo
	}

	// A configmap.Getter to read from the config file
	getConfigFile string
)

// ConfigMap creates a configmap.Map from the *RegInfo and the
// configName passed in.
// If bkInfo is nil then the returned configmap.Map should only be
// used for reading non backend specific parameters, such as global config
func ConfigMap(bkInfo *RegInfo, configName string) *configmap.Map {
	cfg := configmap.New()

	// flag values
	if bkInfo != nil {
		cfg.AddGetter(&optionsValues{bkInfo.Options, false}, configmap.PriorityNormal)
	}

	// flag values for config options
	if bkInfo == nil {
		cfg.AddGetter(&optionsValues{config.Options, true}, configmap.PriorityDefault)
		cfg.AddGetter(&optionsValues{config.Options, false}, configmap.PriorityNormal)
	}

	// specific environment vars
	cfg.AddGetter(configEnvVars(configName), configmap.PriorityNormal)

	// backend specific environment vars
	if bkInfo != nil {
		cfg.AddGetter(optionEnvVars{bkInfo: bkInfo}, configmap.PriorityNormal)
	}

	// config file
	cfg.AddGetter(getConfigFile(configName), configmap.PriorityConfig)

	// default values
	if bkInfo != nil {
		cfg.AddGetter(&optionsValues{bkInfo.Options, true}, configmap.PriorityDefault)
	}

	return cfg
}

// override the values in configMap with the either the flag values or
// the default values
func (ov *optionsValues) Get(key string) (value string, ok bool) {
	opt := ov.options.Get(key)
	if opt != nil && (ov.useDefault || opt.Value != nil) {
		return opt.String(), true
	}

	return "", false
}

// Get a config item from the environment variables if possible
func (configName configEnvVars) Get(key string) (value string, ok bool) {
	envKey := config.SectionOptionToEnv(string(configName), key)

	value, ok = os.LookupEnv(envKey)
	if ok {
		log.Printf("Setting %s=%q for %q from environment variable %s", key, value, configName, envKey)
	}

	return value, ok
}

// Get a config item from the option environment variables if possible
func (oev optionEnvVars) Get(key string) (value string, ok bool) {
	opt := oev.bkInfo.Options.Get(key)
	if opt == nil {
		return "", false
	}

	envKey := config.OptionToEnv(oev.bkInfo.Name + "-" + key)
	value, ok = os.LookupEnv(envKey)
	if ok {
		log.Printf("Setting %s_%s=%q from environment variable %s", oev.bkInfo.Name, key, value, envKey)
	} else if opt.NoPrefix {
		// For options with NoPrefix set, check without prefix too
		envKey := config.OptionToEnv(key)
		value, ok = os.LookupEnv(envKey)
		if ok {
			log.Printf("Setting %s=%q for %q from environment variable %s", key, value, oev.bkInfo.Name, envKey)
		}
	}

	return value, ok
}

// Get a config item from the config file
func (section getConfigFile) Get(key string) (value string, ok bool) {
	value, ok = config.FileGet(string(section), key)
	// Ignore empty lines in the config file
	if value == "" {
		ok = false
	}

	return value, ok
}
