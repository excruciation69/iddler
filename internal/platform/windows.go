//go:build windows

package platform

// Windows: DPAPI for secrets, Run key for autostart, a tray icon via Win32 (no cgo),
// and the window as an Edge "app" window.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// LegacyAccount exists only on macOS (Idler 2003 was Mac-only).
func LegacyAccount() (string, string) { return "", "" }

const Name = "windows"

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	crypt32  = syscall.NewLazyDLL("crypt32.dll")

	pCryptProtectData      = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData    = crypt32.NewProc("CryptUnprotectData")
	pLocalFree             = kernel32.NewProc("LocalFree")
	pSetThreadExecState    = kernel32.NewProc("SetThreadExecutionState")
	pGlobalLock            = kernel32.NewProc("GlobalLock")
	pGlobalUnlock          = kernel32.NewProc("GlobalUnlock")
	pGlobalSize            = kernel32.NewProc("GlobalSize")
	pMoveMemory            = kernel32.NewProc("RtlMoveMemory")
	pGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	pOpenClipboard         = user32.NewProc("OpenClipboard")
	pCloseClipboard        = user32.NewProc("CloseClipboard")
	pGetClipboardData      = user32.NewProc("GetClipboardData")
	pRegisterClassEx       = user32.NewProc("RegisterClassExW")
	pCreateWindowEx        = user32.NewProc("CreateWindowExW")
	pDefWindowProc         = user32.NewProc("DefWindowProcW")
	pGetMessage            = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessage       = user32.NewProc("DispatchMessageW")
	pPostMessage           = user32.NewProc("PostMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pLoadImage             = user32.NewProc("LoadImageW")
	pCreatePopupMenu       = user32.NewProc("CreatePopupMenu")
	pAppendMenu            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu        = user32.NewProc("TrackPopupMenu")
	pDestroyMenu           = user32.NewProc("DestroyMenu")
	pGetCursorPos          = user32.NewProc("GetCursorPos")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pSetTimer              = user32.NewProc("SetTimer")
	pRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	pShellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
)

type win struct{}

func New() Platform { return win{} }

func DataDir() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return filepath.Join(d, "Iddler")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Roaming", "Iddler")
}

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

// ---- secrets: files encrypted with DPAPI (only this Windows user can read them)

type dataBlob struct {
	n    uint32
	data *byte
}

func blob(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{uint32(len(b)), &b[0]}
}

func (d *dataBlob) bytes() []byte {
	out := make([]byte, d.n)
	copy(out, unsafe.Slice(d.data, d.n))
	pLocalFree.Call(uintptr(unsafe.Pointer(d.data)))
	return out
}

type dpapi struct{ dir string }

func (win) Secrets(dir string) Secrets { return dpapi{filepath.Join(dir, "secrets")} }

func (s dpapi) path(k string) string {
	return filepath.Join(s.dir, strings.NewReplacer(":", "_", "/", "_", `\`, "_").Replace(k)+".bin")
}

func (s dpapi) Get(k string) (string, error) {
	enc, err := os.ReadFile(s.path(k))
	if err != nil {
		return "", err
	}
	var out dataBlob
	r, _, err := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(blob(enc))), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return "", err
	}
	return string(out.bytes()), nil
}

func (s dpapi) Set(k, v string) error {
	var out dataBlob
	r, _, err := pCryptProtectData.Call(uintptr(unsafe.Pointer(blob([]byte(v)))), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return err
	}
	os.MkdirAll(s.dir, 0o700)
	return os.WriteFile(s.path(k), out.bytes(), 0o600)
}

func (s dpapi) Delete(k string) error { return os.Remove(s.path(k)) }

// ---- autostart

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func (win) LoginItemEnabled() bool {
	return hidden(exec.Command("reg", "query", runKey, "/v", "Iddler")).Run() == nil
}

func (win) SetLoginItem(on bool) error {
	if !on {
		hidden(exec.Command("reg", "delete", runKey, "/v", "Iddler", "/f")).Run()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return hidden(exec.Command("reg", "add", runKey, "/v", "Iddler", "/t", "REG_SZ", "/d", `"`+exe+`" --background`, "/f")).Run()
}

// ---- keep awake: reset the idle timer every 30s

func (win) KeepAwake() func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			pSetThreadExecState.Call(0x00000003) // ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED
			select {
			case <-stop:
				return
			case <-t.C:
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(stop) }) }
}

func (win) OpenURL(url string) {
	hidden(exec.Command("rundll32", "url.dll,FileProtocolHandler", url)).Start()
}

func (win) Clipboard() string {
	if r, _, _ := pOpenClipboard.Call(0); r == 0 {
		return ""
	}
	defer pCloseClipboard.Call()
	h, _, _ := pGetClipboardData.Call(13) // CF_UNICODETEXT
	if h == 0 {
		return ""
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer pGlobalUnlock.Call(h)
	size, _, _ := pGlobalSize.Call(h)
	if size < 2 || size > 1<<20 {
		return ""
	}
	text := make([]uint16, size/2)
	pMoveMemory.Call(uintptr(unsafe.Pointer(&text[0])), p, size&^1)
	return syscall.UTF16ToString(text)
}

// ---- window: Edge (or Chrome) in app mode, with its own small profile

func openWindow(url, dir string) {
	var candidates []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		if base := os.Getenv(env); base != "" {
			candidates = append(candidates,
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`))
		}
	}
	for _, exe := range candidates {
		if _, err := os.Stat(exe); err == nil {
			exec.Command(exe, "--app="+url, "--window-size=1120,740", "--user-data-dir="+filepath.Join(dir, "window"),
				"--no-first-run", "--disable-features=Translate").Start()
			return
		}
	}
	win{}.OpenURL(url)
}

// ---- tray icon

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type winMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	X, Y    int32
	Private uint32
}

const (
	wmApp       = 0x8000
	wmTray      = wmApp + 1
	wmTimer     = 0x0113
	wmLButtonUp = 0x0202
	wmRButtonUp = 0x0205
	nimAdd      = 0
	nimModify   = 1
	nimDelete   = 2
	nifMessage  = 1
	nifIcon     = 2
	nifTip      = 4
	nifInfo     = 0x10
)

type tray struct {
	cfg     ShellConfig
	hwnd    uintptr
	nid     notifyIconData
	created uint32
	mu      sync.Mutex
}

var current *tray

func (win) Notify(title, body string) {
	t := current
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	n := t.nid
	n.UFlags = nifInfo
	copyUTF16(n.SzInfoTitle[:], title)
	copyUTF16(n.SzInfo[:], body)
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func copyUTF16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
}

func wstr(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func StartShell(c ShellConfig) error {
	icon := filepath.Join(c.Dir, "iddler.ico")
	os.WriteFile(icon, c.Asset, 0o644)
	t := &tray{cfg: c}
	current = t
	ready := make(chan error, 1)
	go t.run(icon, ready)
	if err := <-ready; err != nil {
		return err
	}
	if c.Open {
		openWindow(c.WindowURL(), c.Dir)
	}
	return nil
}

func (t *tray) run(iconPath string, ready chan error) {
	runtime.LockOSThread()
	inst, _, _ := pGetModuleHandle.Call(0)
	cls := wndClassEx{LpfnWndProc: syscall.NewCallback(t.wndProc), HInstance: inst, LpszClassName: wstr("IddlerTray")}
	cls.CbSize = uint32(unsafe.Sizeof(cls))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&cls)))
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(wstr("IddlerTray"))), uintptr(unsafe.Pointer(wstr("Iddler"))),
		0, 0, 0, 0, 0, ^uintptr(2), 0, inst, 0) // HWND_MESSAGE
	if hwnd == 0 {
		ready <- err
		return
	}
	t.hwnd = hwnd
	created, _, _ := pRegisterWindowMessage.Call(uintptr(unsafe.Pointer(wstr("TaskbarCreated"))))
	t.created = uint32(created)
	hIcon, _, _ := pLoadImage.Call(0, uintptr(unsafe.Pointer(wstr(iconPath))), 1, 0, 0, 0x10|0x40)
	t.nid = notifyIconData{HWnd: hwnd, UID: 1, UFlags: nifMessage | nifIcon | nifTip, UCallbackMessage: wmTray, HIcon: hIcon}
	t.nid.CbSize = uint32(unsafe.Sizeof(t.nid))
	copyUTF16(t.nid.SzTip[:], "Iddler")
	pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
	pSetTimer.Call(hwnd, 1, 1500, 0)
	ready <- nil

	var m winMsg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
}

func (t *tray) wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch {
	case msg == wmTray:
		switch lp & 0xffff {
		case wmLButtonUp:
			openWindow(t.cfg.WindowURL(), t.cfg.Dir)
		case wmRButtonUp:
			t.menu()
		}
		return 0
	case msg == wmTimer:
		st := t.cfg.State()
		if st.Quit {
			pPostQuitMessage.Call(0)
			return 0
		}
		if st.Show {
			openWindow(t.cfg.WindowURL(), t.cfg.Dir)
		}
		tip := "Iddler · " + st.Labels["menu."+st.Status]
		t.mu.Lock()
		copyUTF16(t.nid.SzTip[:], tip)
		t.nid.UFlags = nifTip
		pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
		t.mu.Unlock()
		return 0
	case t.created != 0 && uint32(msg) == t.created:
		t.nid.UFlags = nifMessage | nifIcon | nifTip
		pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}

func (t *tray) menu() {
	st := t.cfg.State()
	m, _, _ := pCreatePopupMenu.Call()
	add := func(id uintptr, flags uintptr, text string) {
		pAppendMenu.Call(m, flags, id, uintptr(unsafe.Pointer(wstr(text))))
	}
	add(0, 1, "Iddler · "+st.Labels["menu."+st.Status]) // grayed
	add(0, 0x800, "")
	add(1, 0, st.Labels["menu.open"])
	if st.Active {
		add(3, 0, st.Labels["menu.stopAll"])
	} else {
		add(2, 0, st.Labels["menu.startAll"])
	}
	add(0, 0x800, "")
	add(4, 0, st.Labels["menu.quit"])
	var pt struct{ X, Y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, 0x100|0x80|0x2, uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	pPostMessage.Call(t.hwnd, 0, 0, 0)
	pDestroyMenu.Call(m)
	switch cmd {
	case 1:
		openWindow(t.cfg.WindowURL(), t.cfg.Dir)
	case 2:
		t.cfg.Action("start")
	case 3:
		t.cfg.Action("stop")
	case 4:
		t.cfg.Action("quit")
		pPostQuitMessage.Call(0)
		go t.cfg.OnExit()
	}
}
