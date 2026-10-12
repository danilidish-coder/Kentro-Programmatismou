//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"
)

// A cohesive native, dark interface: no embedded browser or GitHub integration.
func rgb(r, g, b uintptr) uintptr { return r | (g << 8) | (b << 16) }

var (
	bgColor                                              = rgb(12, 21, 35)
	panelColor                                           = rgb(21, 35, 54)
	sideColor                                            = rgb(17, 30, 47)
	fieldColor                                           = rgb(26, 44, 66)
	topColor                                             = rgb(14, 25, 42)
	accentColor                                          = rgb(45, 124, 232)
	textColor                                            = rgb(235, 243, 254)
	dimColor                                             = rgb(139, 165, 197)
	fontTitle, fontHeading, fontBody, fontMono           uintptr
	brushBG, brushPanel, brushSide, brushField, brushTop uintptr
)
var (
	beginPaint      = user32.NewProc("BeginPaint")
	endPaint        = user32.NewProc("EndPaint")
	fillRect        = user32.NewProc("FillRect")
	drawText        = user32.NewProc("DrawTextW")
	isWindowEnabled = user32.NewProc("IsWindowEnabled")
	setTextColor    = gdi32.NewProc("SetTextColor")
	setBkColor      = gdi32.NewProc("SetBkColor")
	setBkMode       = gdi32.NewProc("SetBkMode")
	createBrush     = gdi32.NewProc("CreateSolidBrush")
	createPen       = gdi32.NewProc("CreatePen")
	selectObject    = gdi32.NewProc("SelectObject")
	deleteObject    = gdi32.NewProc("DeleteObject")
	createFont      = gdi32.NewProc("CreateFontW")
	roundRect       = gdi32.NewProc("RoundRect")
)

type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize point
}

type paintStruct struct {
	Hdc             uintptr
	Erase           int32
	Area            rect
	Restore, Update int32
	Reserved        [32]byte
}
type drawItemStruct struct {
	ControlType, ControlID, ItemID, ItemAction, ItemState uint32
	Hwnd, Hdc                                             uintptr
	ItemRect                                              rect
	ItemData                                              uintptr
}

func makeBrush(c uintptr) uintptr { return invoke(createBrush, c) }
func makeFont(height, weight int, face string) uintptr {
	return invoke(createFont, uintptr(int32(-height)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16(face))))
}
func initThemeFonts() {
	if fontBody != 0 {
		return
	}
	fontTitle = makeFont(30, 700, "Segoe UI")
	fontHeading = makeFont(23, 700, "Segoe UI")
	fontBody = makeFont(19, 500, "Segoe UI")
	fontMono = makeFont(17, 400, "Consolas")
	brushBG = makeBrush(bgColor)
	brushPanel = makeBrush(panelColor)
	brushSide = makeBrush(sideColor)
	brushField = makeBrush(fieldColor)
	brushTop = makeBrush(topColor)
}
func releaseTheme() {
	for _, h := range []uintptr{fontTitle, fontHeading, fontBody, fontMono, brushBG, brushPanel, brushSide, brushField, brushTop} {
		if h != 0 {
			invoke(deleteObject, h)
		}
	}
}
func paintFill(dc uintptr, r rect, brush uintptr) {
	invoke(fillRect, dc, uintptr(unsafe.Pointer(&r)), brush)
}
func paintBackground(hwnd uintptr) {
	var ps paintStruct
	dc := invoke(beginPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer invoke(endPaint, hwnd, uintptr(unsafe.Pointer(&ps)))
	var r rect
	invoke(getClientRect, hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := r.R, r.B
	if brushBG == 0 {
		initThemeFonts()
	}
	paintFill(dc, rect{0, 0, w, h}, brushBG)
	paintFill(dc, rect{0, 0, w, 99}, brushTop)
	if w > 20 && h > 110 {
		var sidebar = rect{16, 103, 318, h - 53}
		paintFill(dc, sidebar, brushSide)
		paintFill(dc, rect{331, 103, w - 16, h - 53}, brushPanel)
		y := int32(417)
		if ui.advanced { y = 579 }
		paintFill(dc, rect{345, y, w - 30, h - 66}, brushField)
	}
}
func colorStatic(dc, hwnd uintptr) uintptr {
	invoke(setBkMode, dc, 1)
	clr := textColor
	brush := brushPanel
	switch hwnd {
	case ui.brand:
		clr = accentColor
		brush = brushTop
	case ui.header:
		brush = brushTop
	case ui.subhead:
		clr = dimColor
		brush = brushTop
	case ui.projectTitle:
		clr = dimColor
		brush = brushSide
	case ui.actionTitle, ui.resultsTitle:
		clr = dimColor
		brush = brushPanel
	case ui.footer:
		clr = dimColor
		brush = brushBG
	}
	invoke(setTextColor, dc, clr)
	return brush
}
func colorField(dc uintptr) uintptr {
	invoke(setBkMode, dc, 2)
	invoke(setTextColor, dc, textColor)
	invoke(setBkColor, dc, fieldColor)
	return brushField
}
func drawButton(d *drawItemStruct) {
	if d == nil || d.Hdc == 0 {
		return
	}
	id := int(d.ControlID)
	enabled := invoke(isWindowEnabled, d.Hwnd) != 0
	fill := rgb(35, 55, 78)
	border := rgb(58, 82, 110)
	if id == 101 || id == 103 {
		fill = accentColor
		border = rgb(83, 163, 255)
	}
	if id == 108 {
		fill = rgb(28, 111, 112)
		border = rgb(59, 164, 159)
	}
	if id == 111 {
		fill = rgb(95, 47, 57)
		border = rgb(160, 75, 87)
	}
	if !enabled {
		fill = rgb(35, 45, 58)
		border = rgb(44, 56, 72)
	}
	brush := makeBrush(fill)
	pen := invoke(createPen, 0, 1, border)
	oldBrush := invoke(selectObject, d.Hdc, brush)
	oldPen := invoke(selectObject, d.Hdc, pen)
	rc := d.ItemRect
	invoke(roundRect, d.Hdc, uintptr(rc.L), uintptr(rc.T), uintptr(rc.R), uintptr(rc.B), 14, 14)
	invoke(selectObject, d.Hdc, oldBrush)
	invoke(selectObject, d.Hdc, oldPen)
	invoke(deleteObject, pen)
	invoke(deleteObject, brush)
	invoke(setBkMode, d.Hdc, 1)
	if enabled {
		invoke(setTextColor, d.Hdc, textColor)
	} else {
		invoke(setTextColor, d.Hdc, dimColor)
	}
	// Button IDs are looked up from a static literal table, not parsed from window text.
	caption := buttonCaption(id)
	invoke(drawText, d.Hdc, uintptr(unsafe.Pointer(u16(caption))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rc)), 0x25)
}
func buttonCaption(id int) string {
	switch id {
	case 101:
		return "1.  ΕΙΣΑΓΩΓΗ ΕΡΓΟΥ"
	case 102:
		return "2.  Τι περιέχει το έργο;"
	case 103:
		return "3.  Δοκιμές και κατασκευή"
	case 104:
		return "4.  Αποτελέσματα"
	case 105:
		return "5.  Αναφορά για το ChatGPT"
	case 106:
		return "Αντίγραφο ασφαλείας"
	case 107:
		return "Επαναφορά αντιγράφου"
	case 108:
		return "Έγκριση έκδοσης"
	case 109:
		return "Άνοιγμα φακέλου"
	case 110:
		return "Ανανέωση λίστας"
	case 111:
		return "Ακύρωση δοκιμών"
	case 112:
		return "Επιπλέον δυνατότητες"
	}
	return ""
}

// Panic reporting is essential for a windowsgui executable (no visible console).
// It protects Go callbacks; native access violations require process isolation.
func safeWndProc(hwnd uintptr, m uint32, w, l uintptr) (result uintptr) {
	defer func() {
		if p := recover(); p != nil {
			root := os.Getenv("LOCALAPPDATA")
			if root == "" {
				root = os.TempDir()
			}
			path := filepath.Join(root, "KentroProgrammatismou_Clean", "crash.log")
			_ = os.MkdirAll(filepath.Dir(path), 0700)
			line := fmt.Sprintf("%s  WM=%d  %v\r\n%s\r\n", time.Now().Format(time.RFC3339), m, p, debug.Stack())
			f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if e == nil {
				_, _ = f.WriteString(line)
				_ = f.Close()
			}
			msgbox(hwnd, "Εμφανίστηκε σφάλμα, αλλά η εφαρμογή παραμένει ανοιχτή.\nΚαταγραφή: "+path, "Κέντρο Προγραμματισμού", 0x10)
			result = 0
		}
	}()
	return wndProc(hwnd, m, w, l)
}

// Keep the syscall import explicit to document the native Win32 boundary.
var _ = syscall.Errno(0)
