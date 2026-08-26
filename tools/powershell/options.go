package powershell

// Options configures a PowerShell Manager.
type Options struct {
	Timeout int
	UsePwsh bool // pwsh.exe vs powershell.exe
}
