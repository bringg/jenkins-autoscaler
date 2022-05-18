package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrg/xdg"
	"github.com/go-playground/validator/v10"
	"github.com/hashicorp/go-multierror"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/configstruct"
	"github.com/rclone/rclone/lib/file"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	MetricsNamespace = "jenkins_autoscaler"

	defaultConfigFileName = "jenkins-autoscaler.yml"
)

var (
	// configPath points to the config file
	configPath = makeConfigPath()

	// Options global configuration
	Options = fs.Options{
		{
			Name:     "dry_run",
			Help:     "whether to enable dry run",
			Default:  false,
			NoPrefix: true,
		},
		{
			Name:     "log_level",
			Help:     "minimum log level",
			Default:  "error",
			NoPrefix: true,
		},
		{
			Name:     "run_interval",
			Help:     "interval of the main scaler loop",
			Default:  "1m",
			NoPrefix: true,
		},
		{
			Name:     "gc_run_interval",
			Help:     "interval of the gc loop",
			Default:  "1h",
			NoPrefix: true,
		},
		{
			Name:     "jenkins_url",
			Help:     "jenkins server base url",
			NoPrefix: true,
			Required: true,
		},
		{
			Name:     "jenkins_user",
			Help:     "jenkins username",
			NoPrefix: true,
			Required: true,
		},
		{
			Name:       "jenkins_token",
			Help:       "jenkins api token",
			IsPassword: true,
			NoPrefix:   true,
			Required:   true,
		},
		{
			Name:     "metrics_server_addr",
			Help:     "address of http metrics server",
			Default:  ":8080",
			NoPrefix: true,
		},
		{
			Name:     "controller_node_name",
			Help:     "built-in Jenkins node (aka master)",
			Default:  "Built-In Node",
			NoPrefix: true,
		},
		{
			Name:     "max_nodes",
			Help:     "maximum number of nodes at any given time",
			Default:  1,
			NoPrefix: true,
		},
		{
			Name:     "min_nodes_during_working_hours",
			Help:     "the minimum nodes to keep while inside working hours",
			Default:  2,
			NoPrefix: true,
		},
		{
			Name:     "scale_up_threshold",
			Help:     "usage percentage above which a scale up will occur",
			Default:  70,
			NoPrefix: true,
		},
		{
			Name:     "scale_down_threshold",
			Help:     "usage percentage above which a scale down will occur",
			Default:  30,
			NoPrefix: true,
		},
		{
			Name:     "scale_up_grace_period",
			Help:     "how much time to wait before another scale up can be performed",
			Default:  "5m",
			NoPrefix: true,
		},
		{
			Name:     "scale_down_grace_period",
			Help:     "how much time to wait before another scale down can be performed",
			Default:  "10m",
			NoPrefix: true,
		},
		{
			Name:     "scale_down_grace_period_during_working_hours",
			Help:     "scale down cooldown timer in minutes during working hours",
			Default:  "10m",
			NoPrefix: true,
		},
		{
			Name:     "working_hours_cron_expressions",
			Help:     "working hours range, specified as cron expression",
			Default:  "* 5-17 * * 1-5",
			NoPrefix: true,
		},
		{
			Name:     "disable_working_hours",
			Help:     "ignore working hours when scaling down",
			NoPrefix: true,
		},
	}
)

type ReadOptionsDst interface {
	Name() string
}

// ReadOptions read options from getters and validate if need and set it to options struct
func ReadOptions(src configmap.Mapper, dst ReadOptionsDst) error {
	err := configstruct.Set(src, dst)
	if err != nil {
		return err
	}

	if err := validator.New().Struct(dst); err != nil {
		if err, ok := err.(validator.ValidationErrors); ok {
			var merr error
			for _, e := range err {
				merr = multierror.Append(merr, fmt.Errorf("%s: field '%s' is %s", dst.Name(), e.Field(), e.Tag()))
			}

			return merr
		}

		return err
	}

	return nil
}

// GetConfigPath get the config file path
func GetConfigPath() string {
	return configPath
}

// FileGet gets the config key under section returning the
// the value and true if found and or ("", false) otherwise
func FileGet(section, key string) (string, bool) {
	return viper.GetString(fmt.Sprintf("%s.%s", section, key)), true
}

// SectionOptionToEnv converts a config section and name, e.g. ("my-section","foo-poo") into an environment name
// "JAS_CONFIG_MY_SECTION_FOO_POO"
func SectionOptionToEnv(section, name string) string {
	return "JAS_CONFIG_" + strings.ToUpper(section+"_"+strings.ReplaceAll(name, "-", "_"))
}

// OptionToEnv converts an option name, e.g. "foo-poo" into an
// environment name "JAS_FOO_POO"
func OptionToEnv(name string) string {
	return "JAS_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// SetConfigPath sets new config file path
//
// Checks for empty string, os null device, or special path, all of which indicates in-memory config.
func SetConfigPath(path string) (err error) {
	var cfgPath string
	if path == "" || path == os.DevNull {
		cfgPath = ""
	} else if err = file.IsReserved(path); err != nil {
		return err
	} else if cfgPath, err = filepath.Abs(path); err != nil {
		return err
	}

	configPath = cfgPath

	return nil
}

// Return the path to the configuration file
func makeConfigPath() string {
	if env := os.Getenv("JAS_CONFIG"); env != "" {
		return env
	}

	// Use jenkins-autoscaler.yml from jas executable directory if already existing
	exe, err := os.Executable()
	if err == nil {
		exedir := filepath.Dir(exe)
		cfgpath := filepath.Join(exedir, defaultConfigFileName)
		_, err := os.Stat(cfgpath)
		if err == nil {
			return cfgpath
		}
	}

	// Find user's configuration directory.
	// Prefer XDG config path, with fallback to $HOME/.config.
	// See XDG Base Directory specification
	// https://specifications.freedesktop.org/basedir-spec/latest/),
	xdgdir, err := xdg.ConfigFile(defaultConfigFileName)
	if err != nil {
		panic(err)
	}

	return xdgdir
}

// SetDefaultFromEnv constructs a name from the flag passed in and
// sets the default from the environment if possible
func SetDefaultFromEnv(flags *pflag.FlagSet, name string) {
	envValue, found := os.LookupEnv(OptionToEnv(name))
	if found {
		flag := flags.Lookup(name)
		if flag == nil {
			log.Fatalf("Couldn't find flag --%q", name)
		}

		flag.DefValue = envValue
	}
}
