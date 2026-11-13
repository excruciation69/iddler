// Package platform hides OS differences: secret storage, autostart, notifications,
// clipboard, keeping the computer awake, and the menu bar / tray shell.
package platform

type Secrets interface {
	Get(key string) (string, error)
	Set(key, value string) error
	Delete(key string) error
}

type Platform interface {
	Secrets(dir string) Secrets
	SetLoginItem(on bool) error
	LoginItemEnabled() bool
	KeepAwake() (stop func())
	Notify(title, body string)
	OpenURL(url string)
	Clipboard() string
}

// MenuState is what the menu bar icon / tray shows.
type MenuState struct {
	Lang      string            `json:"lang"`
	Status    string            `json:"status"` // offline | online | idling
	Active    bool              `json:"active"`
	Accounts  int               `json:"accounts"`
	Running   int               `json:"running"`
	SessionMs int64             `json:"sessionMs"`
	MenuTime  bool              `json:"menuTime"`
	HideDock  bool              `json:"hideDock"`
	Blocked   bool              `json:"blocked"`
	Show      bool              `json:"show"`
	Quit      bool              `json:"quit"`
	Labels    map[string]string `json:"labels"`
}

type ShellConfig struct {
	Base   string // http://127.0.0.1:port
	Key    string // API key
	Open   bool   // open the window right away
	State  func() MenuState
	Action func(action string) // start | stop | quit
	OnExit func()              // the user quit from the shell
	Asset  []byte              // menu bar script on macOS, tray icon on Windows
	Icon   []byte              // app icon (.icns) for the macOS helper app
	Dir    string

	Alive    func() bool // the window/menu bar helper checked in recently
	Quitting func() bool
}

func (c ShellConfig) WindowURL() string { return c.Base + "/?k=" + c.Key + "&native=1" }
