// Package theme defines the colours, sizes and icons of Sheaf's dark native UI.
// It is the only place in internal/native that names a colour (archtest TestNativeHygiene).
package theme

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// FrameHex is also used by mygo's window options, which accept colours as strings.
const FrameHex = "#17181b"

// Only the dark theme exists.
var (
	FrameBg    = ui.Hex(FrameHex)
	Island     = FrameBg
	Rail       = ui.Hex("#1b1c20")
	Line       = ui.Hex("#2b2d33")
	IslandRing = Line
	Panel      = Rail
	MenuRing   = Line
	Hover      = ui.Hex("#202227")
	PanelHover = Hover
	StripeOn   = Selection
	Field      = Rail
	Selection  = ui.Hex("#26282e")
	SelDim     = Selection
	Text       = ui.Hex("#d3d6dc")
	TextStrong = ui.Hex("#f4f5f7")
	// These older information-bearing tiers keep their contrast floors; the next grey is only 4.92:1 on Rail.
	Text2      = Text
	Text3      = Text
	Text4      = ui.Hex("#858a95")
	Text5      = Text4             // hashes and paths also use this tier
	Faint      = ui.Hex("#5c616b") // decoration and line numbers only
	Accent     = TextStrong
	AccentHov  = Text
	AccentDown = Text4
	AccentTxt  = TextStrong
	OnAccent   = FrameBg
	Add        = ui.Hex("#a5dcb4")
	AddBg      = ui.Hex("#1d3528")
	AddEdge    = ui.Hex("#2c6a44")
	Del        = ui.Hex("#eba3ab")
	DelBg      = ui.Hex("#3d2228")
	DelEdge    = ui.Hex("#8a3a45")
	Mod        = ui.Hex("#9dbcf0")
	Mark       = ui.Hex("#69b1ff") // the change a diff stepped to: it must stand out among added and deleted lines
	Remote     = ui.Hex("#b189f5")
	Untracked  = Ticket
	Ticket     = ui.Hex("#e9c46a")
	Resolved   = ui.Hex("#b9a7f5")
	DangerTxt  = Del
	ErrText    = Del
	ErrBg      = DelBg
	FilterOn   = Selection
	AddRing    = AddEdge.Alpha(0.15)
	Scrim      = ui.RGBA(0, 0, 0, 0.6)
	Shadow     = ui.RGBA(0, 0, 0, 0.5)

	RefLocalBg   = Selection
	RefLocalTxt  = ui.Hex("#8fd492")
	RefRemoteBg  = Selection
	RefRemoteTxt = ui.Hex("#c7abf8")
	RefTagBg     = Selection
	RefTagTxt    = ui.Hex("#f0cd85")
	HitBg        = Selection
	HitTxt       = TextStrong

	DiffFoldHover = Hover

	// The requirement page's diff stream. The editor tints a changed line by mixing its change colour into the
	// background, so these are the colours whose mix gives the AddBg and DelBg rows.
	StreamAddTint = ui.Hex("#2d8a55")
	StreamDelTint = ui.Hex("#b04556")
	StepOff       = ui.Hex("#34373e") // a stage square not reached
	RowRule       = ui.Hex("#212327") // between the rows of the commits list
	ResolvedText  = ui.Hex("#cfc3fb") // the subject of a merge edited while merging
	BarAdd        = ui.Hex("#5fae78") // the outline's ratio bars
	BarDel        = ui.Hex("#c2606b")
	BarTrack      = ui.Hex("#2a2c31")
	LineHover     = ui.Hex("#4a4d55") // a hairline button's outline under the pointer

)

// Sizes in device-independent pixels for native windows and controls.
const (
	Gap        = 1 // hairline between panes
	RIsland    = 0
	HLine      = 22
	WSelection = 2
	WResizeHit = 7
	HTitleBar  = 40
	WStrip     = 40 // the icon strips left and right
	HStatusBar = 26
	HPaneHead  = 40
	HRow       = 32
	HTree      = 28
	HButton    = 24
	HSelect    = 28 // selects, fields, filter chips and the title bar's buttons
	HMenuItem  = 26
	RSmall     = 0
	RInner     = 0
	RButton    = 0
	RRow       = 0
	RMenu      = 0
	RDialog    = 0
	FSPage     = 18 // a settings page title above its section headings
	FSTitle    = 14 // a commit's subject over its details
	FSBody     = 14
	FSSmall    = 12
	FSGroup    = 13 // a list's group heading, a step above its rows
	FSLabel    = 12
	// The main island keeps at least this much room beside / above the docks.
	MinMain = 260
	MinDock = 160
)

// Theme is the ui theme of the native windows. Lists build inside ListTheme to keep their selection neutral.
func Theme() *ui.Theme {
	t := *ui.DarkTheme()
	t.Background = Island
	t.Surface = Field
	t.SurfaceHover = Hover
	t.SurfacePressed = StripeOn
	t.Border = Line
	t.Text = Text
	t.TextMuted = Text4
	t.Accent = Accent
	t.AccentHover = AccentHov
	t.AccentPressed = AccentDown
	t.AccentText = OnAccent
	t.Danger = Del
	t.Success = Add
	t.Warning = Ticket
	t.InactiveAccent = Accent
	t.Selection = Selection
	t.Focus = TextStrong
	t.FocusRingWidth = 1
	t.Inverse, t.InverseText = Panel, Text
	t.Scrollbar = ui.RGBA(255, 255, 255, 0.22)
	t.Radius = RButton
	t.FontSize = FSBody
	t.Font = UIFont
	return &t
}

// ListTheme is Theme with the chosen row of lists in Selection and the text left as it is.
func ListTheme() *ui.Theme {
	t := Theme()
	t.Accent, t.AccentText, t.InactiveAccent, t.Radius = Selection, Text, Selection, RRow
	return t
}

// DimListTheme keeps the selected row visible while the keyboard is elsewhere.
func DimListTheme() *ui.Theme {
	t := ListTheme()
	t.Accent = SelDim
	return t
}

// TableTheme is the theme a table builds in: rows of HRow as in the lists, chosen in Selection while focused holds the
// keyboard and SelDim otherwise. A check box inside goes back to Theme, or it would vanish into the chosen row.
func TableTheme(focused bool) *ui.Theme {
	t := DimListTheme()
	if focused {
		t = ListTheme()
	}
	t.Spacing = HRow / 8 // ui.Table's rows are 8 units high
	return t
}

// RowText is the colour of muted text on a row: on a selected row every muted colour would sink into the background.
func RowText(selected bool, normal ui.Color) ui.Color {
	if selected {
		return Text
	}
	return normal
}

// Code keeps syntax neutral so that changed lines carry the colour.
func Code() *editor.Theme {
	t := &editor.Theme{
		Background:        Island,
		Text:              Text,
		LineNumber:        Faint,
		ActiveLineNumber:  Text4,
		ActiveLine:        Hover,
		Selection:         Selection,
		InactiveSelection: SelDim,
		Cursor:            TextStrong,
		MatchingBracket:   Text4,
		Match:             Selection,
		CurrentMatch:      Selection,
		Occurrence:        Selection,
		IndentGuide:       Line,
		ActiveIndentGuide: Text4,
		WrapGuide:         Line,
		Whitespace:        Faint,
		Error:             Del,
		Warning:           Ticket,
		Info:              Text4,
		Hint:              Text4,
		Added:             Add,
		Deleted:           Del,
		Modified:          Mod,
	}
	for k, c := range Syntax {
		t.Tokens[k] = editor.TokenStyle{Color: c, Italic: k == editor.TokenComment}
	}
	t.Tokens[editor.TokenStrong].Bold = true
	t.Tokens[editor.TokenHeading].Bold = true
	t.Tokens[editor.TokenEmphasis].Italic = true
	return t
}

// Syntax colours code by token kind, quietly: most tokens are Text, keywords step back a shade, and only strings and
// comments get a hue of their own, kept off green and red so they are not read as additions or deletions. A diff is
// read for what changed, so the change tints carry the colour and the code stays near monochrome.
var Syntax = map[editor.TokenKind]ui.Color{
	editor.TokenText:        Text,
	editor.TokenVariable:    Text,
	editor.TokenProperty:    Text,
	editor.TokenKeyword:     SynKeyword,
	editor.TokenType:        Text,
	editor.TokenFunction:    Text,
	editor.TokenConstant:    SynString,
	editor.TokenNumber:      SynString,
	editor.TokenString:      SynString,
	editor.TokenEscape:      SynString,
	editor.TokenRegexp:      SynString,
	editor.TokenComment:     SynComment,
	editor.TokenOperator:    SynKeyword,
	editor.TokenPunctuation: SynKeyword,
	editor.TokenTag:         SynKeyword,
	editor.TokenAttribute:   Text,
	editor.TokenMeta:        SynKeyword,
	editor.TokenHeading:     TextStrong,
	editor.TokenEmphasis:    Text,
	editor.TokenStrong:      TextStrong,
	editor.TokenLink:        Text,
	editor.TokenCode:        SynString,
	editor.TokenInserted:    Add,
	editor.TokenDeleted:     Del,
	editor.TokenInvalid:     Del,
}

// The three tones of code besides Text.
var (
	SynKeyword = ui.Hex("#a3a9b6") // a step below Text
	SynString  = ui.Hex("#d9c39a") // muted sand
	SynComment = ui.Hex("#979da9") // grey, set in italic
)

// Status is the colour of a file status letter (A M D R C T, ? untracked).
func Status(s string) ui.Color {
	if s == "" {
		return Mod
	}
	switch s[:1] {
	case "A":
		return Add
	case "D":
		return Del
	case "R", "C":
		return Remote
	case "?":
		return Untracked
	}
	return Mod
}
