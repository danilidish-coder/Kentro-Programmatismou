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

// A calm, high-contrast native Windows studio palette (not a browser).
var (
    bgColor = rgb(242, 246, 252)
    panelColor = rgb(255, 255, 255)
    sideColor = rgb(255, 255, 255)
    fieldColor = rgb(241, 246, 252)
    topColor = rgb(255, 255, 255)
    accentColor = rgb(42, 104, 221)
    accentSoft = rgb(231, 240, 255)
    textColor = rgb(28, 43, 66)
    dimColor = rgb(100, 119, 146)
    lineColor = rgb(166, 183, 207)
    successColor = rgb(18, 132, 106)
    fontTitle, fontHeading, fontBody, fontMono, fontButton, fontSmall uintptr
    brushBG, brushPanel, brushSide, brushField, brushTop, brushBrand uintptr
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
	ellipse         = gdi32.NewProc("Ellipse")
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
    if fontBody != 0 { return }
    fontTitle = makeFont(31, 700, "Segoe UI")
    fontHeading = makeFont(19, 650, "Segoe UI")
    fontBody = makeFont(17, 450, "Segoe UI")
    fontButton = makeFont(18, 650, "Segoe UI")
    fontSmall = makeFont(14, 450, "Segoe UI")
    fontMono = makeFont(15, 450, "Consolas")
    brushBG = makeBrush(bgColor)
    brushPanel = makeBrush(panelColor)
    brushSide = makeBrush(sideColor)
    brushField = makeBrush(fieldColor)
    brushTop = makeBrush(topColor)
    brushBrand = makeBrush(accentSoft)
}
func releaseTheme() {
    for _,h:=range []uintptr{fontTitle,fontHeading,fontBody,fontButton,fontSmall,fontMono,
        brushBG,brushPanel,brushSide,brushField,brushTop,brushBrand} {
        if h!=0 {invoke(deleteObject,h)}
    }
}
func paintFill(dc uintptr, r rect, brush uintptr) {
	invoke(fillRect, dc, uintptr(unsafe.Pointer(&r)), brush)
}
func roundedArea(dc uintptr,r rect,fill,border uintptr,radius int) {
    brush:=makeBrush(fill)
    pen:=invoke(createPen,0,1,border)
    oldBrush:=invoke(selectObject,dc,brush)
    oldPen:=invoke(selectObject,dc,pen)
    invoke(roundRect,dc,uintptr(r.L),uintptr(r.T),uintptr(r.R),uintptr(r.B),uintptr(radius),uintptr(radius))
    invoke(selectObject,dc,oldBrush)
    invoke(selectObject,dc,oldPen)
    invoke(deleteObject,pen)
    invoke(deleteObject,brush)
}
func paintBackground(hwnd uintptr) {
    var ps paintStruct
    dc:=invoke(beginPaint,hwnd,uintptr(unsafe.Pointer(&ps)))
    if dc==0{return}
    defer invoke(endPaint,hwnd,uintptr(unsafe.Pointer(&ps)))
    var client rect
    invoke(getClientRect,hwnd,uintptr(unsafe.Pointer(&client)))
    w,h:=client.R,client.B
    if brushBG==0{initThemeFonts()}
    paintFill(dc,rect{0,0,w,h},brushBG)
    paintFill(dc,rect{0,0,w,98},brushTop)
    // A thin brand stroke establishes hierarchy without filling the dashboard.
    paintFill(dc,rect{0,96,w,98},brushBrand)
    mainX:=int32(332)
    if w<950{mainX=309}
    // Solid white cards with subtle border, generous whitespace.
    // Two raised working surfaces with visible outlines and drop shadows.
    roundedArea(dc,rect{18,115,mainX-11,h-45},rgb(205,215,229),rgb(205,215,229),18)
    roundedArea(dc,rect{15,110,mainX-14,h-50},panelColor,lineColor,18)
    roundedArea(dc,rect{mainX-5,115,w-11,h-45},rgb(205,215,229),rgb(205,215,229),18)
    roundedArea(dc,rect{mainX-9,110,w-15,h-50},panelColor,lineColor,18)
    // Brand monogram.
    roundedArea(dc,rect{25,19,73,69},accentSoft,accentSoft,15)
    // Progress rail: a quiet four-step cue. The active stage is in the live label.
    paintFill(dc,rect{mainX+16,148,w-33,150},brushBrand)
}
func colorStatic(dc, hwnd uintptr) uintptr {
    invoke(setBkMode,dc,2)
    clr:=textColor
    brush:=brushPanel
    bg:=panelColor
    switch hwnd {
    case ui.brand:
        clr=accentColor;brush=brushBrand;bg=accentSoft
    case ui.header:
        brush=brushTop;bg=topColor
    case ui.subhead:
        clr=dimColor;brush=brushTop;bg=topColor
    case ui.projectTitle,ui.actionTitle,ui.resultsTitle:
        clr=dimColor
    case ui.stageStatus:
        clr=accentColor
    case ui.footer:
        clr=dimColor;brush=brushBG;bg=bgColor
    case ui.projectDetail:
        clr=accentColor
    case ui.emptyHint:
        clr=dimColor;brush=brushField;bg=fieldColor
    case ui.output,ui.list:
        brush=brushField;bg=fieldColor
    }
    invoke(setTextColor,dc,clr)
    invoke(setBkColor,dc,bg)
    return brush
}
func colorField(dc uintptr) uintptr {
	invoke(setBkMode, dc, 2)
	invoke(setTextColor, dc, textColor)
	invoke(setBkColor, dc, fieldColor)
	return brushField
}
func drawButton(d *drawItemStruct) {
    if d==nil || d.Hdc==0{return}
    id:=int(d.ControlID)
    enabled:=invoke(isWindowEnabled,d.Hwnd)!=0
    pressed:=d.ItemState&0x0001!=0
    focused:=d.ItemState&0x0010!=0
    hovered:=enabled && ui.hoverButton==id
    step:=id==101||id==103||id==115||id==116
    primary:=id==103||id==115
    rc:=d.ItemRect
    if rc.R-rc.L<35||rc.B-rc.T<22{return}

    // Physical button layers: back shadow, elevation, strong visible bezel,
    // upper highlight, and pressed state with reduced offset.
    base,border,foreground:=rgb(248,250,254),rgb(133,155,183),textColor
    topGlow:=rgb(255,255,255)
    shadow:=rgb(181,197,219)
    if primary{
        base,border,foreground=rgb(40,108,223),rgb(16,68,152),rgb(255,255,255)
        topGlow=rgb(82,149,245)
        shadow=rgb(130,155,193)
    } else if id==116 {
        base,border,foreground=rgb(232,243,255),rgb(78,133,196),rgb(26,82,158)
        topGlow=rgb(255,255,255)
    } else if id==111 {
        base,border,foreground=rgb(254,242,242),rgb(197,121,132),rgb(145,56,70)
    }
    if hovered {
        if primary {
            base=rgb(29,90,195);border=rgb(10,52,126);topGlow=rgb(67,135,232)
        } else {
            base=rgb(226,238,253);border=rgb(38,101,193)
            foreground=rgb(18,67,149)
        }
    }
    if !enabled{
        base,border,foreground=rgb(228,234,242),rgb(188,198,211),rgb(115,133,156)
        topGlow=rgb(238,242,248)
        shadow=rgb(206,213,225)
    }
    lift:=int32(4)
    if pressed&&enabled {lift=1;base=rgb(29,80,165)}
    shadowRect:=rect{rc.L+3,rc.T+5,rc.R-3,rc.B}
    roundedArea(d.Hdc,shadowRect,shadow,shadow,15)
    surface:=rect{rc.L+1,rc.T+1+(4-lift),rc.R-2,rc.B-lift}
    roundedArea(d.Hdc,surface,base,border,15)
    highlightRect:=rect{surface.L+4,surface.T+3,surface.R-4,surface.T+5}
    if highlightRect.R>highlightRect.L {
        highlight:=makeBrush(topGlow)
        paintFill(d.Hdc,highlightRect,highlight)
        invoke(deleteObject,highlight)
    }
    if focused&&enabled {
        focus:=rect{surface.L+5,surface.T+7,surface.R-5,surface.B-5}
        outline:=invoke(createPen,0,1,foreground)
        oldPen:=invoke(selectObject,d.Hdc,outline)
        oldBrush:=invoke(selectObject,d.Hdc,invoke(getStockObject,5))
        invoke(roundRect,d.Hdc,uintptr(focus.L),uintptr(focus.T),uintptr(focus.R),uintptr(focus.B),10,10)
        invoke(selectObject,d.Hdc,oldBrush)
        invoke(selectObject,d.Hdc,oldPen)
        invoke(deleteObject,outline)
    }
    caption:=buttonCaption(id)
    if caption==""{return}
    invoke(setBkMode,d.Hdc,1)
    font:=fontBody
    if step{font=fontButton}
    oldFont:=invoke(selectObject,d.Hdc,font)
    invoke(setTextColor,d.Hdc,foreground)
    textRect:=surface
    if step {
        label:="01"
        switch id{case 103:label="02";case 115:label="03";case 116:label="04"}
        diameter:=int32(29)
        if surface.R-surface.L<235 {diameter=25}
        cx:=surface.L+13
        cy:=surface.T+(surface.B-surface.T-diameter)/2
        badgeColor,badgeInk:=rgb(214,229,251),accentColor
        if primary&&enabled {badgeColor,badgeInk=rgb(93,155,242),rgb(255,255,255)}
        if !enabled {badgeColor,badgeInk=rgb(209,218,229),rgb(106,125,149)}
        marker:=rect{cx,cy,cx+diameter,cy+diameter}
        roundedArea(d.Hdc,marker,badgeColor,badgeColor,diameter)
        invoke(setTextColor,d.Hdc,badgeInk)
        invoke(selectObject,d.Hdc,fontSmall)
        invoke(drawText,d.Hdc,uintptr(unsafe.Pointer(u16(label))),uintptr(^uint32(0)),uintptr(unsafe.Pointer(&marker)),0x0025)
        invoke(selectObject,d.Hdc,font)
        invoke(setTextColor,d.Hdc,foreground)
        textRect.L=cx+diameter+10
        textRect.R-=8
    } else {
        textRect.L+=11
        textRect.R-=8
    }
    if pressed&&enabled {textRect.T+=2;textRect.B+=2}
    invoke(drawText,d.Hdc,uintptr(unsafe.Pointer(u16(caption))),uintptr(^uint32(0)),uintptr(unsafe.Pointer(&textRect)),0x0024|0x00008000)
    invoke(selectObject,d.Hdc,oldFont)
}
func buttonCaption(id int) string {
    switch id {
    case 101:return "Εισαγωγή έργου ZIP"
    case 103:return "Έλεγχοι και stress tests"
    case 115:return "Δημιουργία EXE"
    case 116:return "Προαιρετικό Setup"
    case 104:return "Αποτελέσματα"
    case 105:return "Εξαγωγή αναφοράς"
    case 111:return "Ακύρωση εργασίας"
    case 113:return "Αφαίρεση έργου"
    case 114:return "Επαναφορά έργου"
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
