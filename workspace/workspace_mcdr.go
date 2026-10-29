package workspace

import "github.com/mclucy/lucy/version"

// WrapperInfo describes non-jar wrappers around a server directory.
type WrapperInfo struct {
	MCDR *MCDREnv `json:"mcdr,omitempty"`
}

type MCDREnv struct {
	Version version.Version `json:"version"`
	Config  *MCDRConfig     `json:"config,omitempty"`
}

// MCDRConfig is the decoded shape of an MCDR config.yml file.
type MCDRConfig struct {
	Language         string `yaml:"language"`
	WorkingDirectory string `yaml:"working_directory"`
	StartCommand     string `yaml:"start_command"`
	Handler          string `yaml:"handler"`
	Encoding         string `yaml:"encoding"`
	Decoding         string `yaml:"decoding"`
	Rcon             struct {
		Enable   bool   `yaml:"enable"`
		Address  string `yaml:"address"`
		Port     int    `yaml:"port"`
		Password string `yaml:"password"`
	} `yaml:"rcon"`
	PluginDirectories         []string `yaml:"plugin_directories"`
	CatalogueMetaCacheTtl     int      `yaml:"catalogue_meta_cache_ttl"`
	CatalogueMetaFetchTimeout int      `yaml:"catalogue_meta_fetch_timeout"`
	CatalogueMetaUrl          any      `yaml:"catalogue_meta_url"`
	PluginDownloadUrl         any      `yaml:"plugin_download_url"`
	PluginDownloadTimeout     int      `yaml:"plugin_download_timeout"`
	PluginPipInstallExtraArgs any      `yaml:"plugin_pip_install_extra_args"`
	CheckUpdate               bool     `yaml:"check_update"`
	AdvancedConsole           bool     `yaml:"advanced_console"`
	HttpProxy                 any      `yaml:"http_proxy"`
	HttpsProxy                any      `yaml:"https_proxy"`
	Telemetry                 bool     `yaml:"telemetry"`
	DisableConsoleThread      bool     `yaml:"disable_console_thread"`
	DisableConsoleColor       bool     `yaml:"disable_console_color"`
	CustomHandlers            any      `yaml:"custom_handlers"`
	CustomInfoReactors        any      `yaml:"custom_info_reactors"`
	WatchdogThreshold         int      `yaml:"watchdog_threshold"`
	HandlerDetection          bool     `yaml:"handler_detection"`
	Debug                     struct {
		All          bool `yaml:"all"`
		Mcdr         bool `yaml:"mcdr"`
		Handler      bool `yaml:"handler"`
		Reactor      bool `yaml:"reactor"`
		Plugin       bool `yaml:"plugin"`
		Permission   bool `yaml:"permission"`
		Command      bool `yaml:"command"`
		TaskExecutor bool `yaml:"task_executor"`
		Telemetry    bool `yaml:"telemetry"`
	} `yaml:"debug"`
}
