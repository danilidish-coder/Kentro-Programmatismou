//go:build windows

package main

import (
	"context"
	"fmt"
	"kentro.local/programmatismou/engine"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	wmCreate           = 0x0001
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmCommand          = 0x0111
	wmSetFont          = 0x0030
	wmPaint            = 0x000F
	wmTimer            = 0x0113
	emSetSel           = 0x00B1
	emReplaceSel       = 0x00C2
	emScrollCaret      = 0x00B7
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
	wsClipChildren     = 0x02000000
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
	lbDeleteString     = 0x0182
	lbGetCount         = 0x018B
	lbSetTopIndex      = 0x0197
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
	setTimer         = user32.NewProc("SetTimer")
	killTimer        = user32.NewProc("KillTimer")
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
	started                                                                                     time.Time
	current                                                                                     string
	completedPass, completedFail, completedSkipped                                              int
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
	ui.main = invoke(createWindow, 0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16("Κέντρο Προγραμματισμού"))), wsOverlappedWindow|wsClipChildren, 110, 80, 1120, 790, 0, 0, inst, 0)
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
		ui.actionTitle = control("STATIC", "ΔΗΜΙΟΥΡΓΙΑ ΠΡΟΓΡΑΜΜΑΤΟΣ", wsChildVisible, 0, 345, 105, 660, 28)
		ui.resultsTitle = control("STATIC", "ΑΠΟΤΕΛΕΣΜΑΤΑ ΕΛΕΓΧΟΥ", wsChildVisible, 0, 345, 452, 660, 27)
		ui.footer = control("STATIC", "Τοπική λειτουργία • Τα έργα παραμένουν στον υπολογιστή", wsChildVisible, 0, 28, 690, 860, 28)
		ui.list = control("LISTBOX", "", wsChildVisible|wsTabStop|wsVscroll|0x0001|0x0040, 90, 28, 200, 275, 450)
		// Only the actions requested by a non-programmer are on the home screen.
		// Backup/version management still happens inside the local engine.
		btns := []struct {
			id    int
			label string
		}{
			{101, "Εισαγωγή αρχείου ZIP"},
			{103, "Δοκιμές και δημιουργία EXE"},
			{104, "Προβολή αποτελεσμάτων"},
			{105, "Αναφορά για το ChatGPT"},
			{111, "Ακύρωση δοκιμών"},
			{113, "Αφαίρεση έργου"},
			{114, "Επαναφορά αφαιρεμένου"},
		}
		for _, v := range btns {
			ui.buttons[v.id] = control("BUTTON", v.label, wsChildVisible|wsTabStop|bsOwnerDraw, v.id, 0, 0, 10, 10)
		}
		invoke(enableWindow, ui.buttons[111], 0)
		ui.output = control("LISTBOX", "", wsChildVisible|wsVscroll|0x0040, 91, 345, 390, 680, 230)
		invoke(sendMessage, ui.output, wmSetFont, fontBody, 1)
		appendOutput("Εισάγετε ZIP, επιλέξτε έργο και πατήστε Δοκιμές και δημιουργία EXE.")
		invoke(setTimer, hwnd, 1, 1000, 0)
		layout(hwnd)
		refreshList()
		return 0
	case wmApp + 9:
		// Test-only: each message must occupy its own list row.
		if os.Getenv("KENTRO_UI_SMOKE_TEST") == "1" {
			for i := 0; i < 34; i++ {
				appendOutput(fmt.Sprintf("Οπτικός έλεγχος %02d: Σωστή εμφάνιση", i+1))
			}
		}
		return 0
	case wmTimer:
		if ui.busy {
			elapsed := time.Since(ui.started).Round(time.Second)
			caption := fmt.Sprintf("Εκτελείται έλεγχος • %s • %d επιτυχίες • %d προβλήματα", elapsed, ui.completedPass, ui.completedFail)
			invoke(sendMessage, ui.footer, wmSetText, 0, uintptr(unsafe.Pointer(u16(caption))))
		}
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
				if strings.HasPrefix(ev.line, "TEST:") || strings.HasPrefix(ev.line, "STRESS:") || strings.HasPrefix(ev.line, "BUILD:") || strings.HasPrefix(ev.line, "SMOKE:") || strings.HasPrefix(ev.line, "VISUAL:") || strings.HasPrefix(ev.line, "INTEGRITY:") {
					ui.current = ev.line
					if strings.HasSuffix(ev.line, " — PASS") {
						ui.completedPass++
						continue // Technical PASS spam belongs in the full report, not the home page.
					}
					if strings.Contains(ev.line, " — FAIL") {
						ui.completedFail++
					}
					if strings.Contains(ev.line, " — NOT RUN") {
						ui.completedSkipped++
					}
				}
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
			invoke(sendMessage, ui.footer, wmSetText, 0, uintptr(unsafe.Pointer(u16("Η εργασία ολοκληρώθηκε • Πατήστε «Αποτελέσματα»"))))
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
		invoke(killTimer, hwnd, 1)
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
	const mainX = 332
	contentW := w - mainX - 26
	half := (contentW - 12) / 2
	invoke(moveWindow, ui.brand, 24, 20, 44, 40, 1)
	invoke(moveWindow, ui.header, 80, 18, uintptr(w-95), 42, 1)
	invoke(moveWindow, ui.subhead, 81, 61, uintptr(w-100), 25, 1)
	invoke(moveWindow, ui.projectTitle, 26, 121, 270, 26, 1)
	invoke(moveWindow, ui.actionTitle, uintptr(mainX), 121, uintptr(contentW), 27, 1)
	invoke(moveWindow, ui.buttons[101], 24, 154, 278, 58, 1)
	invoke(moveWindow, ui.list, 24, 226, 278, uintptr(h-425), 1)
	invoke(moveWindow, ui.buttons[113], 24, uintptr(h-177), 278, 47, 1)
	invoke(moveWindow, ui.buttons[114], 24, uintptr(h-121), 278, 40, 1)
	invoke(moveWindow, ui.buttons[103], uintptr(mainX), 156, uintptr(contentW), 77, 1)
	invoke(moveWindow, ui.buttons[104], uintptr(mainX), 249, uintptr(half), 55, 1)
	invoke(moveWindow, ui.buttons[105], uintptr(mainX+half+12), 249, uintptr(half), 55, 1)
	invoke(moveWindow, ui.buttons[111], uintptr(mainX), 314, uintptr(contentW), 38, 1)
	invoke(moveWindow, ui.resultsTitle, uintptr(mainX), 374, uintptr(contentW), 27, 1)
	invoke(moveWindow, ui.output, uintptr(mainX+12), 414, uintptr(contentW-24), uintptr(h-492), 1)
	invoke(moveWindow, ui.footer, 24, uintptr(h-38), uintptr(w-48), 25, 1)
	invoke(user32.NewProc("InvalidateRect"), hwnd, 0, 1)
}
