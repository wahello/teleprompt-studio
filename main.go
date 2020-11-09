package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/mum4k/termdash/cell"

	"github.com/mum4k/termdash"
	"github.com/mum4k/termdash/container"
	"github.com/mum4k/termdash/keyboard"
	"github.com/mum4k/termdash/linestyle"
	"github.com/mum4k/termdash/terminal/termbox"
	"github.com/mum4k/termdash/terminal/terminalapi"
	"github.com/mum4k/termdash/widgets/text"
)

const ROOTID = "root"

var selectedChunk uint
var selectedTake int
var isRecordingTake bool

type widgets struct {
	script   *ScriptDisplayWidget
	audio    *AudioDisplayWidget
	chunks   *ChunkListWidget
	controls *text.Text
	takes    *TakeListWidget
}

var ui widgets

type keybind struct {
	key  keyboard.Key
	desc string
}

func getAvailableKeybinds() []keybind {
	var keys []keybind
	if !isRecordingTake {
		keys = append(keys, []keybind{
			{
				key:  keyboard.KeyArrowDown,
				desc: "Next Chunk",
			},
			{
				key:  keyboard.KeyArrowUp,
				desc: "Previous Chunk",
			},
			{
				key:  ' ',
				desc: "Start Take",
			},
			{
				key:  'g',
				desc: "Mark Good",
			},
			{
				key:  'b',
				desc: "Mark Bad",
			},
			{
				key:  'p',
				desc: "Play Selected Take",
			},
		}...)

		if ui.audio.selectionActive {
			keys = append(keys,
				keybind{
					key:  't',
					desc: "New Take from selection",
				},
			)
		}
	} else {
		keys = append(keys, []keybind{
			{
				key:  ' ',
				desc: "End Take",
			},
			{
				key:  'g',
				desc: "End Take & Mark Good",
			},
			{
				key:  'b',
				desc: "End Take & Mark Bad",
			},
		}...)
	}
	return keys
}

func IgnoreValueFormatter(value float64) string {
	return ""
}

func buildLayout(t *termbox.Terminal) *container.Container {
	root, err := container.New(t, container.ID(ROOTID))
	if err != nil {
		log.Fatal(err)
	}

	scriptWidget := &ScriptDisplayWidget{}

	waveformWidget := &AudioDisplayWidget{}
	waveformWidget.stickToEnd = true
	waveformWidget.showDebug = true
	go waveformWidget.animateWaiting()

	chunksWidget := &ChunkListWidget{}

	controlsWidget, err := text.New()
	if err != nil {
		log.Fatal(err)
	}

	takesWidget := &TakeListWidget{}
	ui = widgets{
		script:   scriptWidget,
		audio:    waveformWidget,
		chunks:   chunksWidget,
		controls: controlsWidget,
		takes:    takesWidget,
	}

	layout := []container.Option{
		container.SplitVertical(
			container.Left(
				container.SplitHorizontal(
					container.Top(
						container.Border(linestyle.Light),
						container.BorderTitle("Script"),
						container.PlaceWidget(ui.script),
					),
					container.Bottom(
						container.SplitHorizontal(
							container.Top(
								container.PlaceWidget(ui.controls),
							),
							container.Bottom(
								container.Border(linestyle.Light),
								container.BorderTitle("Audio"),
								container.PlaceWidget(ui.audio),
							),
							container.SplitFixed(3),
						),
					),
					container.SplitPercent(50),
				),
			),
			container.Right(
				container.SplitHorizontal(
					container.Top(
						container.Border(linestyle.Light),
						container.BorderTitle("Chunks"),
						container.PlaceWidget(ui.chunks),
					),
					container.Bottom(
						container.Border(linestyle.Light),
						container.BorderTitle("Takes"),
						container.PlaceWidget(ui.takes),
					),
					container.SplitPercent(50),
				),
			),
			container.SplitPercent(80),
		),
	}

	if err := root.Update(ROOTID, layout...); err != nil {
		log.Fatal(err)
	}

	return root
}

func updateControlsDisplay() {
	ui.controls.Reset()
	keybinds := getAvailableKeybinds()
	for _, bind := range keybinds {
		ui.controls.Write(fmt.Sprintf("%s", bind.key), text.WriteCellOpts(cell.BgColor(cell.ColorWhite), cell.FgColor(cell.ColorBlack)))
		ui.controls.Write(fmt.Sprintf(" %s  ", bind.desc))
	}
}

func globalKeyboardHandler(k *terminalapi.Keyboard) {
	if k.Key == keyboard.KeyEsc || k.Key == keyboard.KeyCtrlC {
		portaudio.Terminate()
		terminal.Close()
		cancelGlobal()
	} else if k.Key == keyboard.KeyArrowUp {
		if isRecordingTake {
			return
		}
		if selectedChunk > 0 {
			selectedChunk -= 1
		}
		chunk := currentSession.Doc.GetChunk(int(selectedChunk))
		selectedTake = len(chunk.Takes) - 1
	} else if k.Key == keyboard.KeyArrowDown {
		if isRecordingTake {
			return
		}
		if selectedChunk < uint(currentSession.Doc.CountChunks()-1) {
			selectedChunk += 1
		}
		chunk := currentSession.Doc.GetChunk(int(selectedChunk))
		selectedTake = len(chunk.Takes) - 1
	} else if k.Key == ' ' {
		if !isRecordingTake {
			startTake()
		} else {
			endTake()
		}
	} else if k.Key == 'g' {
		currentSession.Doc.GetChunk(int(selectedChunk)).Takes[selectedTake].Mark = Good
		if isRecordingTake {
			endTake()
		}
	} else if k.Key == 'b' {
		currentSession.Doc.GetChunk(int(selectedChunk)).Takes[selectedTake].Mark = Bad
		if isRecordingTake {
			endTake()
		}
	} else if k.Key == 't' {
		if ui.audio.selectionActive {
			chunk := currentSession.Doc.GetChunk(int(selectedChunk))
			take := Take{}
			take.Start = ui.audio.selected.Start
			take.End = ui.audio.selected.End
			chunk.Takes = append(chunk.Takes, take)
			selectedTake = len(chunk.Takes) - 1
			ui.audio.Deselect()
		}
	} else if k.Key == 'r' {
		err := EndSession()
		if err != nil {
			log.Print(err)
		}
	} else if k.Key == 'p' {
		take := currentSession.Doc.GetChunk(int(selectedChunk)).Takes[selectedTake]
		go playbackTake(take)
	} else {
		log.Printf("Unknown key pressed: %v", k)
	}

	updateControlsDisplay()
}

func globalMouseHandler(m *terminalapi.Mouse) {
	updateControlsDisplay()
}

var terminal *termbox.Terminal
var ctxGlobal context.Context
var cancelGlobal context.CancelFunc

func main() {
	f, err := os.Create("debug.log")
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(f)
	scriptFile := flag.String("script", "", "Path to the markdown file to use as input.")
	flag.Parse()

	terminal, err = termbox.New(termbox.ColorMode(terminalapi.ColorMode256))
	if err != nil {
		log.Fatal(err)
	}
	defer terminal.Close()
	log.Print("Building layout")
	c := buildLayout(terminal)

	ctxGlobal, cancelGlobal = context.WithCancel(context.Background())

	updateControlsDisplay()

	currentSession = Session{
		Audio: make([]int32, 0, sampleRate),
	}

	log.Print("Reading script")
	err = readScript(*scriptFile)
	if err != nil {
		terminal.Close()
		log.Fatalf("Failed to open file %s: %s", *scriptFile, err)
	}

	StartSession()
	go audioProcessor()

	log.Print("Running termdash")
	if err := termdash.Run(ctxGlobal, terminal, c, termdash.KeyboardSubscriber(globalKeyboardHandler), termdash.MouseSubscriber(globalMouseHandler), termdash.RedrawInterval(10*time.Millisecond)); err != nil {
		log.Fatalf("%s", err)
	}
}
