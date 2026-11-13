// Iddler window, Dock icon and menu bar square for macOS (JavaScript for Automation).
//
// Normally Iddler compiles this file into a small stay-open app ("applet"): macOS then shows it
// in the Dock as Iddler and calls run() once. As a fallback it also runs under
// osascript -l JavaScript menubar.js --child BASE KEY, menu bar only.
ObjC.import('Cocoa');
ObjC.import('WebKit');
ObjC.import('stdlib');

var DIR = __IDDLER_DIR__; // filled in by Iddler: where shell.json with the address and key lives
var APPLET = true, BASE = '', KEY = '';
var app, ctl, item, miStatus, miOpen, miToggle, miQuit, miQuitApp, miClose, miHide;
var win = null, web = null, state = null, labels = {};
var phase = false, ticks = 0, misses = 0, policy = -1, wasActive = false, quitting = false, shown = '';
var IMG = {};

function readConfig() {
  try {
    var s = $.NSString.stringWithContentsOfFileEncodingError(DIR + '/shell.json', 4, null);
    if (s && !s.isNil()) {
      var c = JSON.parse(ObjC.unwrap(s));
      BASE = c.base;
      KEY = c.key;
    }
  } catch (e) {}
}

function get(path) {
  if (!BASE) return null;
  try {
    var url = $.NSURL.URLWithString(BASE + path + (path.indexOf('?') >= 0 ? '&' : '?') + 'k=' + KEY);
    var s = $.NSString.stringWithContentsOfURLEncodingError(url, 4, null);
    if (!s || s.isNil()) return null;
    return JSON.parse(ObjC.unwrap(s));
  } catch (e) {
    return null;
  }
}

function L(key, fallback) { return labels[key] || fallback; }

function fmt(ms) {
  var m = Math.floor(ms / 60000);
  return Math.floor(m / 60) + ':' + (m % 60 < 10 ? '0' : '') + (m % 60);
}

// The status square, drawn like the LED in the app: an outline, lit inside when online.
// Template images follow the menu bar colour (dark or light) by themselves.
function square(lit, faded) {
  var img = $.NSImage.alloc.initWithSize($.NSMakeSize(18, 18));
  img.lockFocus;
  var c = $.NSColor.colorWithSRGBRedGreenBlueAlpha(0, 0, 0, faded ? 0.45 : 1);
  c.setStroke;
  c.setFill;
  var frame = $.NSBezierPath.bezierPathWithRect($.NSMakeRect(3.75, 3.75, 10.5, 10.5));
  frame.lineWidth = 1.5;
  frame.lineJoinStyle = 0; // sharp corners, like the rest of the app
  frame.stroke;
  if (lit) $.NSBezierPath.bezierPathWithRect($.NSMakeRect(6.25, 6.25, 5.5, 5.5)).fill;
  img.unlockFocus;
  img.template = true;
  return img;
}

function setIcon(kind) {
  if (kind === shown) return;
  shown = kind;
  item.button.image = IMG[kind];
}

function showWindow() {
  if (!BASE) return;
  if (win) {
    if (win.isMiniaturized) win.deminiaturize(null);
    win.makeKeyAndOrderFront(null);
    app.activateIgnoringOtherApps(true);
    return;
  }
  var rect = $.NSMakeRect(0, 0, 1160, 760);
  win = $.NSWindow.alloc.initWithContentRectStyleMaskBackingDefer(rect, 15, 2, false);
  win.releasedWhenClosed = false;
  win.title = 'Iddler';
  win.titleVisibility = 1; // the page draws its own header, so the name shows once
  win.titlebarAppearsTransparent = true;
  win.backgroundColor = $.NSColor.colorWithSRGBRedGreenBlueAlpha(11 / 255, 11 / 255, 11 / 255, 1);
  win.appearance = $.NSAppearance.appearanceNamed('NSAppearanceNameDarkAqua');
  win.minSize = $.NSMakeSize(720, 520);

  web = $.WKWebView.alloc.initWithFrameConfiguration(rect, $.WKWebViewConfiguration.alloc.init);
  web.autoresizingMask = 18;
  try { web.setValueForKey($.NSNumber.numberWithBool(false), 'drawsBackground'); } catch (e) {} // no white flash
  win.contentView = web;
  web.loadRequest($.NSURLRequest.requestWithURL($.NSURL.URLWithString(BASE + '/?native=1&k=' + KEY)));

  win.delegate = ctl;
  win.center;
  win.setFrameAutosaveName('IddlerMain');
  win.makeKeyAndOrderFront(null);
  app.activateIgnoringOtherApps(true);
}

// closing the window drops the web view, so only the small engine stays in memory
function dropWindow() {
  if (win) win.delegate = null;
  if (web) {
    web.stopLoading;
    web.loadHTMLStringBaseURL('', null);
    web.removeFromSuperview;
  }
  web = null;
  win = null;
  // step aside, so a click on the Dock icon activates us again and reopens the window
  if (APPLET && policy === 0) app.hide(null);
}

function setPolicy(want) {
  if (want === policy) return;
  policy = want;
  app.setActivationPolicy(want); // 0: regular, in the Dock · 1: accessory, menu bar only
  if (win) {
    win.makeKeyAndOrderFront(null);
    app.activateIgnoringOtherApps(true);
  }
}

function quitAll() {
  quitting = true;
  get('/api/menu/quit');
  $.exit(0);
}

function poll() {
  var b = get('/api/menu');
  if (!b) {
    misses++;
    if (misses % 3 === 0) readConfig(); // the engine may have restarted on a new port
    if (misses > 40) $.exit(0); // the engine is gone; it starts a fresh helper when it comes back
    state = null;
    miStatus.title = 'Iddler · ' + L('menu.noEngine', 'Not connected');
    return;
  }
  misses = 0;
  if (b.quit) {
    quitting = true;
    $.exit(0);
  }
  state = b;
  labels = b.labels || {};
  setPolicy(APPLET && !b.hideDock ? 0 : 1);
  if (b.show) showWindow();
  item.button.title = b.active && b.menuTime ? ' ' + fmt(b.sessionMs) : '';
  miStatus.title = 'Iddler · ' + L('menu.' + b.status, b.status);
  miOpen.title = L('menu.open', 'Open Iddler');
  miToggle.title = b.active ? L('menu.stopAll', 'Stop idling') : L('menu.startAll', 'Start idling');
  miToggle.enabled = b.accounts > 0;
  miQuit.title = miQuitApp.title = L('menu.quit', 'Quit');
}

function tick() {
  ticks++;
  phase = !phase;
  if (ticks % 2 === 0) poll();
  var s = state;
  if (!s) setIcon('faded');
  else if (s.status === 'idling') setIcon(s.blocked ? (ticks % 4 < 2 ? 'lit' : 'dark') : (phase ? 'lit' : 'dark'));
  else setIcon(s.status === 'online' ? 'lit' : 'dark');

  // a click on the Dock icon activates the app: bring the window back if it was closed
  var active = app.isActive;
  if (APPLET && policy === 0 && active && !wasActive && !win) showWindow();
  wasActive = active;
}

function setup() {
  app = $.NSApplication.sharedApplication;
  if (APPLET) policy = 0; // an applet starts in the Dock; the first poll applies the setting
  else setPolicy(1); // under osascript the Dock would show "osascript": menu bar only

  ObjC.registerSubclass({
    name: 'IddlerShell',
    superclass: 'NSObject',
    protocols: ['NSWindowDelegate'],
    methods: {
      'openWindow:': { types: ['void', ['id']], implementation: function () { showWindow(); } },
      'toggleIdle:': {
        types: ['void', ['id']],
        implementation: function () { get(state && state.active ? '/api/menu/stop' : '/api/menu/start'); poll(); }
      },
      'quitApp:': { types: ['void', ['id']], implementation: function () { quitAll(); } },
      'tick:': { types: ['void', ['id']], implementation: function () { tick(); } },
      'windowWillClose:': { types: ['void', ['id']], implementation: function () { dropWindow(); } },
      // quitting from the Dock menu ends the whole app, not just the window
      'willQuit:': { types: ['void', ['id']], implementation: function () { if (!quitting) get('/api/menu/quit'); } }
    }
  });
  ctl = $.IddlerShell.alloc.init;
  $.NSNotificationCenter.defaultCenter.addObserverSelectorNameObject(ctl, 'willQuit:', 'NSApplicationWillTerminateNotification', null);

  IMG = { lit: square(true, false), dark: square(false, false), faded: square(false, true) };
  item = $.NSStatusBar.systemStatusBar.statusItemWithLength(-1);
  item.button.imagePosition = 2; // image left of the optional timer
  item.button.font = $.NSFont.monospacedDigitSystemFontOfSizeWeight(12, 0);
  setIcon('faded');

  var menu = $.NSMenu.alloc.init;
  menu.autoenablesItems = false;
  function add(m, title, action, key) {
    var mi = $.NSMenuItem.alloc.initWithTitleActionKeyEquivalent(title, action || null, key || '');
    if (action) mi.target = ctl;
    m.addItem(mi);
    return mi;
  }
  miStatus = add(menu, 'Iddler');
  miStatus.enabled = false;
  menu.addItem($.NSMenuItem.separatorItem);
  miOpen = add(menu, 'Open Iddler', 'openWindow:');
  miToggle = add(menu, 'Start idling', 'toggleIdle:');
  menu.addItem($.NSMenuItem.separatorItem);
  miQuit = add(menu, 'Quit', 'quitApp:');
  item.menu = menu;

  // main menu: the app menu with Quit, and Edit so Cmd+C / Cmd+V / Cmd+A work in the window
  var mainMenu = $.NSMenu.alloc.init;
  var appItem = $.NSMenuItem.alloc.init;
  mainMenu.addItem(appItem);
  var appMenu = $.NSMenu.alloc.initWithTitle('Iddler');
  miHide = appMenu.addItemWithTitleActionKeyEquivalent('Hide Iddler', 'hide:', 'h');
  miClose = appMenu.addItemWithTitleActionKeyEquivalent('Close', 'performClose:', 'w');
  appMenu.addItem($.NSMenuItem.separatorItem);
  miQuitApp = add(appMenu, 'Quit', 'quitApp:', 'q');
  appItem.submenu = appMenu;
  var editItem = $.NSMenuItem.alloc.init;
  mainMenu.addItem(editItem);
  var edit = $.NSMenu.alloc.initWithTitle('Edit');
  [['Undo', 'undo:', 'z'], ['Redo', 'redo:', 'Z'], ['Cut', 'cut:', 'x'], ['Copy', 'copy:', 'c'], ['Paste', 'paste:', 'v'], ['Select All', 'selectAll:', 'a']].forEach(function (d) {
    edit.addItemWithTitleActionKeyEquivalent(d[0], d[1], d[2]);
  });
  editItem.submenu = edit;
  app.mainMenu = mainMenu;

  $.NSTimer.scheduledTimerWithTimeIntervalTargetSelectorUserInfoRepeats(0.5, ctl, 'tick:', null, true);
  poll();
}

function run(argv) {
  APPLET = !(argv && argv.length && argv[0] === '--child');
  if (APPLET) readConfig();
  else {
    BASE = argv[1];
    KEY = argv[2];
  }
  setup();
  if (!APPLET) app.run; // osascript needs its own event loop; an applet already has one
}

// a click on the Dock icon while the app is already in front
function reopen() {
  showWindow();
}
