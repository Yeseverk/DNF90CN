//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	transparentMode = 1

	odsSelected    = 0x0001
	odsDisabled    = 0x0004
	odsFocus       = 0x0010
	bsOwnerDraw    = 0x0000000B
	wsBorder       = 0x00800000
	esReadOnly     = 0x00000800
	wmDrawItem     = 0x002B
	wmCtlColor     = 0x0138
	wmCtlColorEdit = 0x0133
	wmCtlColorBtn  = 0x0135
)

var (
	launcherColorBackground = colorRef(0xF7, 0xF9, 0xFC)
	launcherColorSurface    = colorRef(0xFF, 0xFF, 0xFF)
	launcherColorInput      = colorRef(0xFF, 0xFF, 0xFF)
	launcherColorBorder     = colorRef(0xD6, 0xDD, 0xE8)
	launcherColorDivider    = colorRef(0xE5, 0xEA, 0xF1)
	launcherColorText       = colorRef(0x1E, 0x29, 0x3B)
	launcherColorMuted      = colorRef(0x66, 0x70, 0x85)
	launcherColorAccent     = colorRef(0x2D, 0x6C, 0xDF)
	launcherColorAccentDark = colorRef(0x22, 0x58, 0xB8)
	launcherColorAccentSoft = colorRef(0xE8, 0xF1, 0xFF)
	launcherColorSuccess    = colorRef(0x14, 0x80, 0x4A)
	launcherColorDanger     = colorRef(0xC0, 0x39, 0x2B)
	launcherColorDisabled   = colorRef(0xA3, 0xAC, 0xB9)

	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procCreatePen        = gdi32.NewProc("CreatePen")
	procCreateFontW      = gdi32.NewProc("CreateFontW")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procSetBkColor       = gdi32.NewProc("SetBkColor")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procRoundRect        = gdi32.NewProc("RoundRect")
	procFillRect         = user32.NewProc("FillRect")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procDrawFocusRect    = user32.NewProc("DrawFocusRect")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
)

func colorRef(red, green, blue uint32) uint32 {
	return red | green<<8 | blue<<16
}

type launcherTheme struct {
	backgroundBrush uintptr
	surfaceBrush    uintptr
	inputBrush      uintptr
	borderBrush     uintptr
	dividerBrush    uintptr
	accentBrush     uintptr
	accentSoftBrush uintptr
	successBrush    uintptr
	dangerBrush     uintptr

	titleFont  uintptr
	bodyFont   uintptr
	labelFont  uintptr
	buttonFont uintptr
	smallFont  uintptr
}

var globalTheme launcherTheme

func initLauncherTheme() error {
	globalTheme = launcherTheme{
		backgroundBrush: solidBrush(launcherColorBackground),
		surfaceBrush:    solidBrush(launcherColorSurface),
		inputBrush:      solidBrush(launcherColorInput),
		borderBrush:     solidBrush(launcherColorBorder),
		dividerBrush:    solidBrush(launcherColorDivider),
		accentBrush:     solidBrush(launcherColorAccent),
		accentSoftBrush: solidBrush(launcherColorAccentSoft),
		successBrush:    solidBrush(launcherColorSuccess),
		dangerBrush:     solidBrush(launcherColorDanger),
		titleFont:       createLauncherFont(-24, 650),
		bodyFont:        createLauncherFont(-15, 400),
		labelFont:       createLauncherFont(-14, 600),
		buttonFont:      createLauncherFont(-15, 600),
		smallFont:       createLauncherFont(-13, 400),
	}
	if globalTheme.backgroundBrush == 0 || globalTheme.bodyFont == 0 {
		releaseLauncherTheme()
		return windows.ERROR_NOT_ENOUGH_MEMORY
	}
	return nil
}

func releaseLauncherTheme() {
	for _, object := range []uintptr{
		globalTheme.backgroundBrush,
		globalTheme.surfaceBrush,
		globalTheme.inputBrush,
		globalTheme.borderBrush,
		globalTheme.dividerBrush,
		globalTheme.accentBrush,
		globalTheme.accentSoftBrush,
		globalTheme.successBrush,
		globalTheme.dangerBrush,
		globalTheme.titleFont,
		globalTheme.bodyFont,
		globalTheme.labelFont,
		globalTheme.buttonFont,
		globalTheme.smallFont,
	} {
		if object != 0 {
			procDeleteObject.Call(object)
		}
	}
	globalTheme = launcherTheme{}
}

func solidBrush(color uint32) uintptr {
	brush, _, _ := procCreateSolidBrush.Call(uintptr(color))
	return brush
}

func createLauncherFont(height, weight int32) uintptr {
	fontName := utf16Ptr("Microsoft YaHei UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(uint32(scalePixel(height))),
		0,
		0,
		0,
		uintptr(uint32(weight)),
		0,
		0,
		0,
		uintptr(1),
		0,
		0,
		uintptr(5),
		0,
		uintptr(unsafe.Pointer(fontName)),
	)
	return font
}

type drawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	ItemHandle uintptr
	DC         uintptr
	Rect       windows.Rect
	ItemData   uintptr
}

func drawLauncherButton(item *drawItemStruct) {
	if item == nil || item.DC == 0 {
		return
	}
	controlID := uint16(item.CtlID)
	background := launcherColorSurface
	border := launcherColorBorder
	textColor := launcherColorText
	if controlID == controlPrimary {
		background = launcherColorAccent
		border = launcherColorAccent
		textColor = launcherColorSurface
		if item.ItemState&odsSelected != 0 {
			background = launcherColorAccentDark
		}
	} else if controlID == controlSlot1 || controlID == controlSlot2 {
		selected := (controlID == controlSlot1 && globalLauncher.activeSlot == 0) ||
			(controlID == controlSlot2 && globalLauncher.activeSlot == 1)
		if selected {
			background = launcherColorAccentSoft
			border = launcherColorAccent
			textColor = launcherColorAccentDark
		} else {
			background = launcherColorBackground
		}
	} else if item.ItemState&odsSelected != 0 {
		background = colorRef(0xF1, 0xF4, 0xF8)
	}
	if item.ItemState&odsDisabled != 0 {
		background = colorRef(0xE9, 0xED, 0xF3)
		border = colorRef(0xD5, 0xDB, 0xE5)
		textColor = launcherColorDisabled
	}

	brush := solidBrush(background)
	pen, _, _ := procCreatePen.Call(0, uintptr(scalePixel(1)), uintptr(border))
	oldBrush, _, _ := procSelectObject.Call(item.DC, brush)
	oldPen, _, _ := procSelectObject.Call(item.DC, pen)
	procRoundRect.Call(
		item.DC,
		uintptr(item.Rect.Left),
		uintptr(item.Rect.Top),
		uintptr(item.Rect.Right),
		uintptr(item.Rect.Bottom),
		uintptr(scalePixel(8)),
		uintptr(scalePixel(8)),
	)
	procSelectObject.Call(item.DC, oldBrush)
	procSelectObject.Call(item.DC, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)

	procSetBkMode.Call(item.DC, transparentMode)
	procSetTextColor.Call(item.DC, uintptr(textColor))
	font := globalTheme.buttonFont
	oldFont, _, _ := procSelectObject.Call(item.DC, font)
	text := getWindowText(item.ItemHandle)
	rect := item.Rect
	procDrawTextW.Call(
		item.DC,
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(^uint32(0)),
		uintptr(unsafe.Pointer(&rect)),
		uintptr(0x0001|0x0004|0x0020|0x8000),
	)
	procSelectObject.Call(item.DC, oldFont)
	if item.ItemState&odsFocus != 0 {
		focusRect := item.Rect
		focusRect.Left += scalePixel(4)
		focusRect.Top += scalePixel(4)
		focusRect.Right -= scalePixel(4)
		focusRect.Bottom -= scalePixel(4)
		procDrawFocusRect.Call(item.DC, uintptr(unsafe.Pointer(&focusRect)))
	}
}

func themeControlColor(message uint32, dc uintptr, control uintptr) uintptr {
	if dc == 0 {
		return 0
	}
	if message == wmCtlColorEdit {
		procSetTextColor.Call(dc, uintptr(launcherColorText))
		procSetBkColor.Call(dc, uintptr(launcherColorInput))
		return globalTheme.inputBrush
	}
	if message == wmCtlColorBtn {
		procSetTextColor.Call(dc, uintptr(launcherColorText))
		procSetBkMode.Call(dc, transparentMode)
		return globalTheme.backgroundBrush
	}
	procSetBkMode.Call(dc, transparentMode)
	color := launcherColorText
	brush := globalTheme.backgroundBrush
	switch control {
	case globalLauncher.accentBar:
		color = launcherColorAccent
		brush = globalTheme.accentBrush
	case globalLauncher.divider:
		color = launcherColorDivider
		brush = globalTheme.dividerBrush
	case globalLauncher.title:
		color = launcherColorText
	case globalLauncher.subtitle:
		color = launcherColorMuted
	case globalLauncher.clientLabel,
		globalLauncher.accountLabel,
		globalLauncher.usernameLabel,
		globalLauncher.passwordLabel:
		color = launcherColorMuted
	case globalLauncher.status:
		globalLauncher.mu.Lock()
		failed := globalLauncher.stageFailed
		complete := globalLauncher.progressStage == launcherStageComplete
		globalLauncher.mu.Unlock()
		switch {
		case failed:
			color = launcherColorDanger
		case complete:
			color = launcherColorSuccess
		default:
			color = launcherColorMuted
		}
	case globalLauncher.stageLabels[0],
		globalLauncher.stageLabels[1],
		globalLauncher.stageLabels[2],
		globalLauncher.stageLabels[3]:
		color = launcherStageColor(control)
	case globalLauncher.clientPath:
		procSetBkMode.Call(dc, 2)
		procSetBkColor.Call(dc, uintptr(launcherColorInput))
		return globalTheme.inputBrush
	}
	procSetTextColor.Call(dc, uintptr(color))
	return brush
}

func launcherStageColor(control uintptr) uint32 {
	globalLauncher.mu.Lock()
	stage := globalLauncher.progressStage
	failed := globalLauncher.stageFailed
	globalLauncher.mu.Unlock()
	index := -1
	for candidate, handle := range globalLauncher.stageLabels {
		if handle == control {
			index = candidate
			break
		}
	}
	if index < 0 {
		return launcherColorMuted
	}
	step := launcherStage(index + 1)
	if failed && step == stage {
		return launcherColorDanger
	}
	if stage == launcherStageComplete || step < stage {
		return launcherColorSuccess
	}
	if step == stage {
		return launcherColorAccent
	}
	return launcherColorMuted
}
