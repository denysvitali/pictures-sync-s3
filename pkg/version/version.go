package version

import "runtime/debug"

const zeroPseudoVersion = "v0.0.0-00010101000000-000000000000"

var (
	// Version and BuildDate are populated by release builds with -ldflags.
	Version   = "dev"
	BuildDate = ""
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	Dirty     bool   `json:"dirty"`
	GoVersion string `json:"go_version,omitempty"`
	Module    string `json:"module,omitempty"`
}

func Get() Info {
	info := Info{
		Version:   Version,
		BuildDate: BuildDate,
	}

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		info.GoVersion = buildInfo.GoVersion
		info.Module = buildInfo.Main.Path

		if info.Version == "" || info.Version == "dev" {
			if isUsableBuildVersion(buildInfo.Main.Version) {
				info.Version = buildInfo.Main.Version
			}
		}

		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.Commit = setting.Value
				if info.Version == "" || info.Version == "dev" {
					info.Version = setting.Value
				}
			case "vcs.time":
				if info.BuildDate == "" {
					info.BuildDate = setting.Value
				}
			case "vcs.modified":
				info.Dirty = setting.Value == "true"
			}
		}
	}

	if info.Version == "" {
		info.Version = "dev"
	}

	return info
}

func isUsableBuildVersion(value string) bool {
	return value != "" && value != "(devel)" && value != zeroPseudoVersion
}
