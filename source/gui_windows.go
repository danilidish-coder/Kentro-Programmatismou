//go:build windows

package main

import (
	"context"
	"fmt"
	"net/url"
	"kentro.local/programmatismou/engine"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmCreate           = 0x0001
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmCommand          = 0x0111
	wmSetFont          = 0x0030
	wmPaint            = 0x000F
	wmEraseBkgnd       = 0x0014
	wmDrawItem         = 0x002B
	wmGetMinMaxInfo    = 0x0024
	wsTabStop          = 0x00010000
	wmCtlColorStatic   = 0x0138
	wmCtlColorListbox  = 0x0134
	wmCtlColorEdit     = 0x0133
	bsOwnerDraw        = 0x0000000B
	wmGetText          = 0x000D
	wmSetText          = 0x000C
	wmApp              = 0x8000
	wsOverlappedWindow = 0x00CF0000
	wsChildVisible     = 0x50000000
	wsBorder           = 0x00800000
	esMultiline        = 0x0004
	esAutoVscroll      = 0x0040
	esReadonly         = 0x0800
	wsVscroll          = 0x00200000
	lbAddString        = 0x0180
	lbResetContent     = 0x0184
	lbGetCurSel        = 0x0188
	lbSetCurSel        = 0x0186
	mbYesNo            = 0x00000004
	mbIconWarning      = 0x30
	idYes              = 6
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	gdi32            = syscall.NewLazyDLL("gdi32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	createWindow     = user32.NewProc("CreateWindowExW")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	showWindow       = user32.NewProc("ShowWindow")
	updateWindow     = user32.NewProc("UpdateWindow")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postQuit         = user32.NewProc("PostQuitMessage")
	messageBox       = user32.NewProc("MessageBoxW")
	sendMessage      = user32.NewProc("SendMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	enableWindow     = user32.NewProc("EnableWindow")
	moveWindow       = user32.NewProc("MoveWindow")
	getClientRect    = user32.NewProc("GetClientRect")
	getModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	copyNativeMemory = kernel32.NewProc("RtlMoveMemory")
	getStockObject   = gdi32.NewProc("GetStockObject")
	loadCursor       = user32.NewProc("LoadCursorW")
)

func u16(x string) *uint16                             { return syscall.StringToUTF16Ptr(x) }
func invoke(p *syscall.LazyProc, a ...uintptr) uintptr { r, _, _ := p.Call(a...); return r }

type point struct{ X, Y int32 }
type msg struct {
	Hwnd           uintptr
	Message        uint32
	Pad            uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	LPrivate       uint32
	Pad2           uint32
}
type rect struct{ L, T, R, B int32 }
type wndClass struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Class                        *uint16
	IconSmall                          uintptr
}
type event struct {
	line    string
	done    bool
	refresh bool
}

var ui struct {
	main, list, output, header, brand, subhead, projectTitle, actionTitle, resultsTitle, footer uintptr
	buttons                                                                                     map[int]uintptr
	fonts                                                                                       []uintptr
	store                                                                                       *engine.Store
	ids                                                                                         []string
	logs                                                                                        string
	busy                                                                                        bool
	cancel                                                                                      context.CancelFunc
	jobCtx                                                                                      context.Context
	mu                                                                                          sync.Mutex
	pending                                                                                     []event
	advanced                                                                                    bool
}

func startApp() {
	runtime.LockOSThread()
	store, e := engine.NewStore("")
	if e != nil {
		msgbox(0, "Αποτυχία δημιουργίας τοπικής αποθήκης: "+e.Error(), "Σφάλμα", 0x10)
		return
	}
	ui.store = store
	cls := u16("KentroProgrammatismouNative")
	inst := invoke(getModuleHandle, 0)
	cursor := invoke(loadCursor, 0, 32512)
	wc := wndClass{Size: uint32(unsafe.Sizeof(wndClass{})), WndProc: syscall.NewCallback(safeWndProc), Instance: inst, Cursor: cursor, Background: makeBrush(bgColor), Class: cls}
	if invoke(registerClass, uintptr(unsafe.Pointer(&wc))) == 0 {
		msgbox(0, "Αδυναμία εκκίνησης του παραθύρου", "Σφάλμα", 0x10)
		return
	}
	ui.main = invoke(createWindow, 0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16("Κέντρο Προγραμματισμού"))), wsOverlappedWindow, 110, 80, 1120, 790, 0, 0, inst, 0)
	if ui.main == 0 {
		return
	}
	invoke(showWindow, ui.main, 1)
	invoke(updateWindow, ui.main)
	var m msg
	for {
		v := invoke(getMessage, uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if v == 0 || v == ^uintptr(0) {
			break
		}
		invoke(translateMessage, uintptr(unsafe.Pointer(&m)))
		invoke(dispatchMessage, uintptr(unsafe.Pointer(&m)))
	}
}
func msgbox(owner uintptr, text, title string, flags uintptr) uintptr {
	return invoke(messageBox, owner, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), flags)
}
func control(class, text string, style uintptr, id int, x, y, w, h int) uintptr {
	inst := invoke(getModuleHandle, 0)
	hwnd := invoke(createWindow, 0, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))), style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), ui.main, uintptr(id), inst, 0)
	invoke(sendMessage, hwnd, wmSetFont, fontBody, 1)
	return hwnd
}
func wndProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmCreate:
		ui.main = hwnd
		ui.buttons = make(map[int]uintptr)
		initThemeFonts()
		ui.brand = control("STATIC", "ΚΠ", wsChildVisible, 0, 26, 20, 45, 34)
		ui.header = control("STATIC", "Κέντρο Προγραμματισμού", wsChildVisible, 0, 84, 20, 850, 42)
		invoke(sendMessage, ui.header, wmSetFont, fontTitle, 1)
		invoke(sendMessage, ui.brand, wmSetFont, fontHeading, 1)
		ui.subhead = control("STATIC", "ΜΟΝΟ ΣΤΟΝ ΥΠΟΛΟΓΙΣΤΗ ΣΟΥ  •  ΕΚΔΟΣΗ "+engine.ProductVersion, wsChildVisible, 0, 86, 61, 800, 24)
		ui.projectTitle = control("STATIC", "ΤΑ ΕΡΓΑ ΜΟΥ", wsChildVisible, 0, 28, 105, 250, 28)
		ui.actionTitle = control("STATIC", "ΑΚΟΛΟΥΘΗΣΤΕ ΤΑ ΒΗΜΑΤΑ", wsChildVisible, 0, 345, 105, 660, 28)
		ui.resultsTitle = control("STATIC", "ΤΙ ΣΥΜΒΑΙΝΕΙ ΣΤΟ ΕΡΓΟ ΜΟΥ", wsChildVisible, 0, 345, 452, 660, 27)
		ui.footer = control("STATIC", "Τοπική λειτουργία • Τα έργα παραμένουν στον υπολογιστή", wsChildVisible, 0, 28, 690, 860, 28)
		ui.list = control("LISTBOX", "", wsChildVisible|wsTabStop|wsVscroll|0x0001|0x0040, 90, 28, 200, 275, 450)
		btns := []struct {
			id    int
			label string
		}{
			{101, "＋  Εισαγωγή έργου ZIP"}, {102, "◫  Ανάλυση έργου"}, {103, "▶  Εκτέλεση όλων των ελέγχων"},
			{104, "▤  Προβολή αποτελεσμάτων"}, {105, "⇧  Εξαγωγή αναφοράς"}, {106, "▣  Αντίγραφο ασφαλείας"},
			{107, "↶  Επαναφορά αντιγράφου"}, {108, "✓  Έγκριση σταθερής έκδοσης"}, {109, "▱  Άνοιγμα φακέλου έργου"}, {110, "↻  Ανανέωση λίστας"},
			{111, "Ακύρωση δοκιμών"}, {112, "Περισσότερες δυνατότητες"},
		}
		for _, v := range btns {
			ui.buttons[v.id] = control("BUTTON", v.label, wsChildVisible|wsTabStop|bsOwnerDraw, v.id, 0, 0, 10, 10)
		}
		invoke(enableWindow, ui.buttons[111], 0)
		for _, id := range []int{106, 107, 108, 109, 110} {
			invoke(showWindow, ui.buttons[id], 0)
		}
		ui.output = control("EDIT", "Επιλέξτε αρχείο ZIP για εισαγωγή ή έργο από τη λίστα.", wsChildVisible|wsVscroll|esMultiline|esAutoVscroll|esReadonly, 91, 345, 490, 680, 230)
		ui.logs = "Καλώς ήρθατε στο Κέντρο Προγραμματισμού.\r\n\r\nΒήμα 1: Εισάγετε το έργο σας (ZIP).\r\nΒήμα 2: Δείτε τι χρειάζεται το έργο.\r\nΒήμα 3: Εκτελέστε ελέγχους και κατασκευή.\r\nΒήμα 4: Δείτε τα αποτελέσματα."
		invoke(sendMessage, ui.output, wmSetFont, fontBody, 1)
		layout(hwnd)
		refreshList()
		return 0
	case wmSize:
		layout(hwnd)
		return 0
	case wmGetMinMaxInfo:
		if l != 0 {
			var info minMaxInfo
			invoke(copyNativeMemory, uintptr(unsafe.Pointer(&info)), l, unsafe.Sizeof(info))
			info.MinTrackSize = point{X: 920, Y: 680}
			invoke(copyNativeMemory, l, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
		}
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		paintBackground(hwnd)
		return 0
	case wmDrawItem:
		if l != 0 {
			var item drawItemStruct
			invoke(copyNativeMemory, uintptr(unsafe.Pointer(&item)), l, unsafe.Sizeof(item))
			drawButton(&item)
		}
		return 1
	case wmCtlColorStatic:
		return colorStatic(w, l)
	case wmCtlColorEdit, wmCtlColorListbox:
		return colorField(w)
	case wmCommand:
		if w>>16 == 0 {
			onButton(int(w & 0xffff))
		}
		return 0
	case wmApp + 3:
		ui.mu.Lock()
		events := append([]event{}, ui.pending...)
		ui.pending = nil
		ui.mu.Unlock()
		refresh := false
		finished := false
		for _, ev := range events {
			if ev.line != "" {
				appendOutput(ev.line)
			}
			if ev.refresh {
				refresh = true
			}
			if ev.done {
				finished = true
			}
		}
		if refresh {
			refreshList()
		}
		if finished {
			ui.busy = false
			if ui.cancel != nil {
				ui.cancel()
				ui.cancel = nil
			}
			ui.jobCtx = nil
			for id, btn := range ui.buttons {
				if id != 111 {
					invoke(enableWindow, btn, 1)
				} else {
					invoke(enableWindow, btn, 0)
				}
			}
		}
		return 0
	case wmDestroy:
		releaseTheme()
		invoke(postQuit, 0)
		return 0
	}
	return invoke(defWindowProc, hwnd, uintptr(m), w, l)
}
func layout(hwnd uintptr) {
	if ui.list == 0 {
		return
	}
	var r rect
	invoke(getClientRect, hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := int(r.R-r.L), int(r.B-r.T)
	if w < 850 {
		w = 850
	}
	if h < 630 {
		h = 630
	}
	mainX := 345
	contentW := w - mainX - 28
	half := (contentW - 12) / 2
	resultsTop := 378
	if ui.advanced {
		resultsTop = 540
	}
	invoke(moveWindow, ui.brand, 27, 21, 43, 40, 1)
	invoke(moveWindow, ui.header, 85, 18, uintptr(w-100), 42, 1)
	invoke(moveWindow, ui.subhead, 87, 61, uintptr(w-110), 25, 1)
	invoke(moveWindow, ui.projectTitle, 28, 113, 285, 28, 1)
	invoke(moveWindow, ui.actionTitle, uintptr(mainX), 113, uintptr(contentW), 28, 1)
	invoke(moveWindow, ui.resultsTitle, uintptr(mainX), uintptr(resultsTop), uintptr(contentW), 28, 1)
	invoke(moveWindow, ui.list, 28, 202, 275, uintptr(h-285), 1)
	invoke(moveWindow, ui.footer, 28, uintptr(h-39), uintptr(w-58), 26, 1)
	invoke(moveWindow, ui.buttons[101], 28, 148, 275, 47, 1)
	for i, id := range []int{102, 103, 104, 105} {
		col, row := i%2, i/2
		x := mainX + col*(half+12)
		y := 149 + row*73
		invoke(moveWindow, ui.buttons[id], uintptr(x), uintptr(y), uintptr(half), 62, 1)
	}
	invoke(moveWindow, ui.buttons[112], uintptr(mainX), 296, uintptr(half), 48, 1)
	invoke(moveWindow, ui.buttons[111], uintptr(mainX+half+12), 296, uintptr(half), 48, 1)
	if ui.advanced {
		for i, id := range []int{106, 107, 108, 109, 110} {
			col, row := i%2, i/2
			x := mainX + col*(half+12)
			y := 359 + row*51
			invoke(moveWindow, ui.buttons[id], uintptr(x), uintptr(y), uintptr(half), 43, 1)
		}
	}
	outY := resultsTop + 39
	outH := h - outY - 65
	if outH < 82 {
		outH = 82
	}
	invoke(moveWindow, ui.output, uintptr(mainX+14), uintptr(outY), uintptr(contentW-28), uintptr(outH), 1)
	invoke(user32.NewProc("InvalidateRect"), hwnd, 0, 1)
}
func queue(ev event) {
	ui.mu.Lock()
	ui.pending = append(ui.pending, ev)
	ui.mu.Unlock()
	invoke(postMessage, ui.main, wmApp+3, 0, 0)
}
func appendOutput(s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	ui.logs += "\r\n" + s
	if len(ui.logs) > 28000 {
		ui.logs = ui.logs[len(ui.logs)-25000:]
	}
	invoke(sendMessage, ui.output, wmSetText, 0, uintptr(unsafe.Pointer(u16(ui.logs))))
}
func refreshList() {
	if ui.list == 0 || ui.store == nil {
		return
	}
	ps, e := ui.store.List()
	if e != nil {
		appendOutput("Σφάλμα λίστας: " + e.Error())
		return
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ImportedAt.After(ps[j].ImportedAt) })
	ui.ids = nil
	invoke(sendMessage, ui.list, lbResetContent, 0, 0)
	for _, p := range ps {
		ui.ids = append(ui.ids, p.ID)
		txt := fmt.Sprintf("%s  |  %s  |  %s", p.Name, p.Version, p.Language)
		invoke(sendMessage, ui.list, lbAddString, 0, uintptr(unsafe.Pointer(u16(txt))))
	}
	if len(ui.ids) > 0 {
		invoke(sendMessage, ui.list, lbSetCurSel, 0, 0)
	}
}
func selected() string {
	n := invoke(sendMessage, ui.list, lbGetCurSel, 0, 0)
	if int(n) >= len(ui.ids) || n == ^uintptr(0) {
		msgbox(ui.main, "Επιλέξτε ένα έργο από τη λίστα.", "Επιλογή έργου", 0x40)
		return ""
	}
	return ui.ids[int(n)]
}
func begin(task func(func(string))) {
	if ui.busy {
		return
	}
	ui.busy = true
	ui.jobCtx, ui.cancel = context.WithCancel(context.Background())
	for id, bt := range ui.buttons {
		if id != 111 {
			invoke(enableWindow, bt, 0)
		} else {
			invoke(enableWindow, bt, 1)
		}
	}
	go func() { defer queue(event{done: true, refresh: true}); task(func(s string) { queue(event{line: s}) }) }()
}
func displayZIPName(path string) string {
	base := filepath.Base(path)
	if decoded, e := url.PathUnescape(base); e == nil && decoded != "" {
		return decoded
	}
	return base
}

func onButton(id int) {
	if id == 112 {
		ui.advanced = !ui.advanced
		for _, adv := range []int{106, 107, 108, 109, 110} {
			if ui.advanced {
				invoke(showWindow, ui.buttons[adv], 1)
			} else {
				invoke(showWindow, ui.buttons[adv], 0)
			}
		}
		layout(ui.main)
		return
	}
	if id == 111 {
		if ui.cancel != nil {
			ui.cancel()
			appendOutput("Ζητήθηκε ακύρωση ελέγχου")
		}
		return
	}
	if id == 110 {
		refreshList()
		return
	}
	if ui.busy {
		return
	}
	if id == 101 {
		file := openZip()
		if file == "" {
			return
		}
		preview, er := engine.PreviewZIP(file)
		if er != nil {
			msgbox(ui.main, "Το ZIP απορρίφθηκε: "+er.Error(), "Έλεγχος εισαγωγής", 0x10)
			return
		}
		message := fmt.Sprintf("Αρχείο: %s\nΠεριεχόμενο: %d αρχεία (%.1f MB ασυμπίεστα)\nΈτοιμες οδηγίες δοκιμής: %t\n\nΘέλετε να εισαχθεί ως νέο έργο;", displayZIPName(file), preview.Files, float64(preview.SizeBytes)/1048576, preview.ContainsManifest)
		if msgbox(ui.main, message, "Προεπισκόπηση ZIP", mbYesNo|0x40) != idYes {
			return
		}
		begin(func(log func(string)) {
			log("Παραλαβή έργου: " + displayZIPName(file))
			p, e := ui.store.ImportZIP(file)
			if e != nil {
				log("ΑΠΟΤΥΧΙΑ: " + e.Error())
			} else {
				log("Επιτυχής εισαγωγή έργου: " + p.Name + " — " + p.Language)
			}
		})
		return
	}
	pID := selected()
	if pID == "" {
		return
	}
	switch id {
	case 102:
		p, m, e := ui.store.Get(pID)
		if e != nil {
			appendOutput(e.Error())
			return
		}
		appendOutput(fmt.Sprintf("Έργο: %s\r\nΓλώσσα: %s\r\nΈκδοση: %s\r\nΔοκιμές: %d\r\nStress: %d\r\nΣυνταγή EXE: %t\r\nΈλεγχος εκκίνησης: %t", p.Name, p.Language, p.Version, len(m.Tests), len(m.Stress), m.Build != nil, m.Smoke != nil))
	case 103:
		if msgbox(ui.main, "Θα εκτελεστεί ο κώδικας του επιλεγμένου έργου στον υπολογιστή σας. Συνεχίζουμε ΜΟΝΟ αν το ZIP προέρχεται από έμπιστη πηγή;", "Επιβεβαίωση εκτέλεσης", mbYesNo|mbIconWarning) != idYes {
			return
		}
		begin(func(log func(string)) {
			r, e := ui.store.RunAll(ui.jobCtx, pID, log)
			if e != nil {
				log("ΣΦΑΛΜΑ: " + e.Error())
				return
			}
			if r.Blocked {
								missing := false
				for _, step := range r.Results {
					if step.Status == "NOT RUN" && strings.Contains(step.Reason, "Δεν βρέθηκε το απαραίτητο εργαλείο") {
						missing = true
					}
				}
				if missing {
					log("Χρειάζεται προετοιμασία υπολογιστή: λείπει εργαλείο που απαιτεί το έργο. Δεν σημαίνει ότι ο κώδικας απέτυχε.")
				}
				log("Οι υποχρεωτικοί έλεγχοι δεν ολοκληρώθηκαν. Η έκδοση παραμένει δοκιμαστική.")
			} else {
				log("Όλοι οι υποχρεωτικοί έλεγχοι ολοκληρώθηκαν — επιτρέπεται η έγκριση")
			}
		})
	case 104:
		r, e := ui.store.LatestRun(pID)
		if e != nil {
			appendOutput(e.Error())
			return
		}
		p, _, e := ui.store.Get(pID)
		if e != nil {
			appendOutput(e.Error())
			return
		}
		appendOutput(engine.Summary(r, p))
	case 105:
		if msgbox(ui.main, "Η αναφορά περιλαμβάνει όνομα έργου, αποτελέσματα και βασικά στοιχεία συστήματος. Δεν περιλαμβάνει πηγαίο κώδικα ή ακατέργαστα logs. Ελέγξτε τη πριν τη στείλετε.", "Εξαγωγή αποτελεσμάτων", mbYesNo|0x40) != idYes {
			return
		}
		begin(func(log func(string)) {
			p, e := ui.store.ExportLatest(pID)
			if e != nil {
				log("Αποτυχία εξαγωγής: " + e.Error())
			} else {
				log("Δημιουργήθηκε πακέτο αναφοράς: " + p)
				exec.Command("explorer.exe", filepath.Dir(p)).Start()
			}
		})
	case 106:
		begin(func(log func(string)) {
			p, e := ui.store.Backup(pID)
			if e != nil {
				log("Αποτυχία αντιγράφου: " + e.Error())
			} else {
				log("Ασφαλές αντίγραφο: " + p)
			}
		})
	case 107:
		if msgbox(ui.main, "Θα δημιουργηθεί ΝΕΟ ανεξάρτητο έργο από το πιο πρόσφατο αντίγραφο. Δεν θα αντικατασταθεί κανένα παλιό αρχείο.", "Επαναφορά", mbYesNo|0x40) != idYes {
			return
		}
		begin(func(log func(string)) {
			p, e := ui.store.RestoreNewest(pID)
			if e != nil {
				log("Αποτυχία: " + e.Error())
			} else {
				log("Επαναφορά σε νέο έργο: " + p.Name)
			}
		})
	case 108:
		begin(func(log func(string)) {
			p, e := ui.store.Approve(pID)
			if e != nil {
				log("ΑΠΟΚΛΕΙΣΜΟΣ: " + e.Error())
			} else {
				log("Εγκρίθηκε σταθερή έκδοση και αποθηκεύτηκε: " + p)
			}
		})
	case 109:
		p, e := ui.store.Base(pID)
		if e == nil {
			exec.Command("explorer.exe", p).Start()
		}
	}
}
func openZip() string {
	// File chooser runs in a separate PowerShell process. A native dialog fault
	// cannot bring down the main application or corrupt its project store.
	file, err := chooseZIPWindows()
	if err != nil {
		appendOutput("ΣΦΑΛΜΑ επιλογής αρχείου: " + err.Error())
		msgbox(ui.main, "Δεν ήταν δυνατή η επιλογή ZIP. Η εφαρμογή παραμένει ανοιχτή.\n\n"+err.Error(), "Επιλογή ZIP", 0x10)
		return ""
	}
	return strings.TrimSpace(file)
}
