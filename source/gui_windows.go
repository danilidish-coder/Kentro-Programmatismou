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
	main, list, output, header, brand, subhead, projectTitle, actionTitle, resultsTitle, footer, stageStatus uintptr
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
	stage                                                                                       string
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
	screenWidth:=int(invoke(user32.NewProc("GetSystemMetrics"),0))
    screenHeight:=int(invoke(user32.NewProc("GetSystemMetrics"),1))
    windowWidth,windowHeight:=1120,790
    if screenWidth>0 && screenWidth<windowWidth+48 {windowWidth=screenWidth-36}
    if screenHeight>0 && screenHeight<windowHeight+82 {windowHeight=screenHeight-80}
    if windowWidth<860{windowWidth=860}
    if windowHeight<590{windowHeight=590}
    left,top:=25,25
    if screenWidth>windowWidth{left=(screenWidth-windowWidth)/2}
    if screenHeight>windowHeight+35{top=(screenHeight-windowHeight-35)/2}
    ui.main = invoke(createWindow, 0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16("Κέντρο Προγραμματισμού"))), wsOverlappedWindow|wsClipChildren, uintptr(left), uintptr(top), uintptr(windowWidth), uintptr(windowHeight), 0, 0, inst, 0)
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
		ui.subhead = control("STATIC", "ΤΟΠΙΚΟ ΕΡΓΑΣΤΗΡΙΟ ΕΦΑΡΜΟΓΩΝ  ·  ΕΚΔΟΣΗ "+engine.ProductVersion, wsChildVisible, 0, 86, 61, 800, 24)
		ui.projectTitle = control("STATIC", "ΤΑ ΕΡΓΑ ΜΟΥ", wsChildVisible, 0, 28, 105, 250, 28)
		ui.actionTitle = control("STATIC", "ΡΟΗ ΕΡΓΑΣΙΑΣ", wsChildVisible, 0, 345, 105, 660, 28)
		ui.stageStatus = control("STATIC", "1. Εισαγωγή → 2. Έλεγχοι → 3. EXE → 4. Setup (προαιρετικό)", wsChildVisible, 0, 345, 144, 650, 23)
		ui.resultsTitle = control("STATIC", "ΑΝΑΦΟΡΑ ΚΑΙ ΠΡΟΟΔΟΣ", wsChildVisible, 0, 345, 430, 660, 27)
		ui.footer = control("STATIC", "Τοπική λειτουργία • Τα έργα παραμένουν στον υπολογιστή", wsChildVisible, 0, 28, 690, 860, 28)
		for _,section:=range []uintptr{ui.projectTitle,ui.actionTitle,ui.resultsTitle} {
            invoke(sendMessage,section,wmSetFont,fontSmall,1)
        }
        invoke(sendMessage,ui.stageStatus,wmSetFont,fontBody,1)
        invoke(sendMessage,ui.subhead,wmSetFont,fontSmall,1)
        invoke(sendMessage,ui.footer,wmSetFont,fontSmall,1)
		ui.list = control("LISTBOX", "", wsChildVisible|wsTabStop|wsVscroll|0x0001|0x0040, 90, 28, 200, 275, 450)
		// Only the actions requested by a non-programmer are on the home screen.
		// Backup/version management still happens inside the local engine.
		btns := []struct {
			id    int
			label string
		}{
			{101, "Εισαγωγή αρχείου ZIP"},
			{103, "Έλεγχοι και stress tests"},
			{115, "Δημιουργία EXE"},
			{116, "Προαιρετικό Setup"},
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
		appendOutput("Ακολουθήστε τη σειρά: εισαγωγή → έλεγχοι/stress → αναφορά → EXE → προαιρετικό Setup.")
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
			caption := fmt.Sprintf("%s • %s • %d επιτυχίες • %d προβλήματα", ui.stage, elapsed, ui.completedPass, ui.completedFail)
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
			info.MinTrackSize = point{X: 860, Y: 590}
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
        } else if int(w & 0xffff)==90 && w>>16==1 {
            updateProjectStage()
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
					showStage(ui.stage + "  •  " + ev.line)
					if strings.HasSuffix(ev.line, " — PASS") {
						ui.completedPass++
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
			invoke(sendMessage, ui.footer, wmSetText, 0, uintptr(unsafe.Pointer(u16("Η εργασία ολοκληρώθηκε • Δείτε την αναφορά και προχωρήστε μόνο όταν είστε έτοιμοι"))))
			showStage(ui.stage + " — ολοκληρώθηκε")
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
			updateProjectStage()
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
	if h < 520 {
		h = 520
	}
    mainX:=332
    if w<950{mainX=309}
    contentW:=w-mainX-22
    half:=(contentW-12)/2
    invoke(moveWindow,ui.brand,24,20,44,40,1)
    invoke(moveWindow,ui.header,80,18,uintptr(w-95),42,1)
    invoke(moveWindow,ui.subhead,81,61,uintptr(w-100),25,1)
    if h<705 {
        invoke(moveWindow,ui.projectTitle,24,101,268,25,1)
        invoke(moveWindow,ui.actionTitle,uintptr(mainX),97,uintptr(contentW),24,1)
        invoke(moveWindow,ui.stageStatus,uintptr(mainX),124,uintptr(contentW),21,1)
        invoke(moveWindow,ui.buttons[101],22,136,uintptr(mainX-34),48,1)
        listHeight:=h-340
        if listHeight<175{listHeight=175}
        invoke(moveWindow,ui.list,22,194,uintptr(mainX-34),uintptr(listHeight),1)
        invoke(moveWindow,ui.buttons[113],22,uintptr(h-126),uintptr(mainX-34),42,1)
        invoke(moveWindow,ui.buttons[114],22,uintptr(h-78),uintptr(mainX-34),36,1)
        invoke(moveWindow,ui.buttons[103],uintptr(mainX),153,uintptr(contentW),49,1)
        invoke(moveWindow,ui.buttons[104],uintptr(mainX),211,uintptr(half),42,1)
        invoke(moveWindow,ui.buttons[105],uintptr(mainX+half+12),211,uintptr(half),42,1)
        invoke(moveWindow,ui.buttons[115],uintptr(mainX),261,uintptr(half),49,1)
        invoke(moveWindow,ui.buttons[116],uintptr(mainX+half+12),261,uintptr(half),49,1)
        invoke(moveWindow,ui.buttons[111],uintptr(mainX),319,uintptr(contentW),31,1)
        invoke(moveWindow,ui.resultsTitle,uintptr(mainX),361,uintptr(contentW),26,1)
        invoke(moveWindow,ui.output,uintptr(mainX+10),397,uintptr(contentW-20),uintptr(h-444),1)
    }else{
        invoke(moveWindow,ui.projectTitle,26,121,270,26,1)
        invoke(moveWindow,ui.actionTitle,uintptr(mainX),115,uintptr(contentW),25,1)
        invoke(moveWindow,ui.stageStatus,uintptr(mainX),145,uintptr(contentW),22,1)
        invoke(moveWindow,ui.buttons[101],24,154,uintptr(mainX-30),58,1)
        invoke(moveWindow,ui.list,24,226,uintptr(mainX-30),uintptr(h-425),1)
        invoke(moveWindow,ui.buttons[113],24,uintptr(h-177),uintptr(mainX-30),47,1)
        invoke(moveWindow,ui.buttons[114],24,uintptr(h-121),uintptr(mainX-30),40,1)
        invoke(moveWindow,ui.buttons[103],uintptr(mainX),175,uintptr(contentW),66,1)
        invoke(moveWindow,ui.buttons[104],uintptr(mainX),253,uintptr(half),46,1)
        invoke(moveWindow,ui.buttons[105],uintptr(mainX+half+12),253,uintptr(half),46,1)
        invoke(moveWindow,ui.buttons[115],uintptr(mainX),311,uintptr(half),56,1)
        invoke(moveWindow,ui.buttons[116],uintptr(mainX+half+12),311,uintptr(half),56,1)
        invoke(moveWindow,ui.buttons[111],uintptr(mainX),378,uintptr(contentW),36,1)
        invoke(moveWindow,ui.resultsTitle,uintptr(mainX),432,uintptr(contentW),27,1)
        invoke(moveWindow,ui.output,uintptr(mainX+12),469,uintptr(contentW-24),uintptr(h-543),1)
    }
    invoke(moveWindow,ui.footer,24,uintptr(h-35),uintptr(w-48),25,1)
	invoke(user32.NewProc("InvalidateRect"), hwnd, 0, 1)
}
