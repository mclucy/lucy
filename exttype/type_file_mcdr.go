package exttype

import "github.com/mclucy/lucy/tools"

type FileMcdrPluginIdentifier struct {
	Id          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description struct {
		EnUs string `json:"en_us"`
		ZhCn string `json:"zh_cn"`
	} `json:"description"`
	Author       tools.OneOrMore[string] `json:"author"`
	Link         string                  `json:"link"`
	Dependencies map[string]string       `json:"dependencies"`
	Resources    []string                `json:"-"`
}

type FileMcdrConfig struct {
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
	PluginDirectories         []string    `yaml:"plugin_directories"`
	CatalogueMetaCacheTtl     int         `yaml:"catalogue_meta_cache_ttl"`
	CatalogueMetaFetchTimeout int         `yaml:"catalogue_meta_fetch_timeout"`
	CatalogueMetaUrl          interface{} `yaml:"catalogue_meta_url"`
	PluginDownloadUrl         interface{} `yaml:"plugin_download_url"`
	PluginDownloadTimeout     int         `yaml:"plugin_download_timeout"`
	PluginPipInstallExtraArgs interface{} `yaml:"plugin_pip_install_extra_args"`
	CheckUpdate               bool        `yaml:"check_update"`
	AdvancedConsole           bool        `yaml:"advanced_console"`
	HttpProxy                 interface{} `yaml:"http_proxy"`
	HttpsProxy                interface{} `yaml:"https_proxy"`
	Telemetry                 bool        `yaml:"telemetry"`
	DisableConsoleThread      bool        `yaml:"disable_console_thread"`
	DisableConsoleColor       bool        `yaml:"disable_console_color"`
	CustomHandlers            interface{} `yaml:"custom_handlers"`
	CustomInfoReactors        interface{} `yaml:"custom_info_reactors"`
	WatchdogThreshold         int         `yaml:"watchdog_threshold"`
	HandlerDetection          bool        `yaml:"handler_detection"`
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
