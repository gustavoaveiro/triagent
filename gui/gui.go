// Package gui provides the desktop interface for triagent, built with
// Fyne so the same binary runs natively on Windows, macOS and Linux.
package gui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	coreapp "github.com/gustavoaveiro/triagent/internal/app"
	"github.com/gustavoaveiro/triagent/internal/progress"
	"github.com/gustavoaveiro/triagent/internal/triage"
)

// Run opens the main window and blocks until the user closes it.
func Run() {
	a := app.New()
	w := a.NewWindow("Triagent")

	inEntry := widget.NewEntry()
	inEntry.SetPlaceHolder("Folder to scan")
	outEntry := widget.NewEntry()
	outEntry.SetPlaceHolder("Folder to move/copy flagged files into")

	modelEntry := widget.NewEntry()
	modelEntry.SetText(coreapp.DefaultAssetPath("model.onnx"))
	libEntry := widget.NewEntry()
	libEntry.SetText(coreapp.DefaultAssetPath(coreapp.DefaultLibraryName()))

	workersEntry := widget.NewEntry()
	workersEntry.SetText(strconv.Itoa(runtime.NumCPU()))

	gpuCheck := widget.NewCheck("Prefer GPU (falls back to CPU automatically)", nil)
	gpuCheck.SetChecked(true)
	dryRunCheck := widget.NewCheck("Dry run (score only, don't move/copy)", nil)

	highEntry := widget.NewEntry()
	highEntry.SetText(fmt.Sprintf("%.2f", triage.DefaultThresholds.High))
	lowEntry := widget.NewEntry()
	lowEntry.SetText(fmt.Sprintf("%.2f", triage.DefaultThresholds.Low))

	excludeEntry := widget.NewEntry()
	excludeEntry.SetPlaceHolder("Extra patterns to skip, comma-separated")
	useDefaultExcludesCheck := widget.NewCheck("Skip common noise (.git, node_modules, __pycache__, *.tmp, ...)", nil)
	useDefaultExcludesCheck.SetChecked(true)

	status := widget.NewLabel("")
	progressBar := widget.NewProgressBar()
	progressBar.Hide()

	openReportBtn := widget.NewButton("Open report", nil)
	openReportBtn.Disable()

	browseFolder := func(target *widget.Entry) *widget.Button {
		return widget.NewButton("Browse…", func() {
			dialog.ShowFolderOpen(func(u fyne.ListableURI, err error) {
				if err != nil || u == nil {
					return
				}
				target.SetText(u.Path())
			}, w)
		})
	}
	browseFile := func(target *widget.Entry) *widget.Button {
		return widget.NewButton("Browse…", func() {
			dialog.ShowFileOpen(func(u fyne.URIReadCloser, err error) {
				if err != nil || u == nil {
					return
				}
				defer u.Close()
				target.SetText(u.URI().Path())
			}, w)
		})
	}

	var runBtn *widget.Button
	runBtn = widget.NewButton("Run triage", func() {
		workers, err := strconv.Atoi(workersEntry.Text)
		if err != nil || workers < 1 {
			dialog.ShowError(fmt.Errorf("workers must be a positive number"), w)
			return
		}
		high, err := strconv.ParseFloat(highEntry.Text, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("high threshold must be a number"), w)
			return
		}
		low, err := strconv.ParseFloat(lowEntry.Text, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("low threshold must be a number"), w)
			return
		}

		tracker := progress.NewTracker()
		cfg := coreapp.Config{
			InDir:             inEntry.Text,
			OutDir:            outEntry.Text,
			Workers:           workers,
			DryRun:            dryRunCheck.Checked,
			Progress:          tracker,
			Excludes:          coreapp.ParseExcludePatterns(excludeEntry.Text),
			NoDefaultExcludes: !useDefaultExcludesCheck.Checked,
			ModelPath:         modelEntry.Text,
			LibraryPath:       libEntry.Text,
			PreferGPU:         gpuCheck.Checked,
			Thresholds:        triage.Thresholds{High: high, Low: low},
		}

		runBtn.Disable()
		openReportBtn.Disable()
		status.SetText("Loading model…")
		progressBar.SetValue(0)
		progressBar.Show()

		runDone := make(chan struct{})
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-runDone:
					return
				case <-ticker.C:
					snap := tracker.Snapshot()
					fyne.Do(func() {
						progressBar.SetValue(snap.Percent())
						status.SetText(progress.RenderLabel(snap))
					})
				}
			}
		}()

		go func() {
			summary, err := coreapp.Run(cfg)
			close(runDone)

			fyne.Do(func() {
				runBtn.Enable()
				if err != nil {
					progressBar.Hide()
					status.SetText("Failed: " + err.Error())
					dialog.ShowError(err, w)
					return
				}
				progressBar.SetValue(1)
				status.SetText(fmt.Sprintf("Done: %d files processed using %s. Report: %s",
					summary.Total, summary.Provider, summary.ReportPath))
				openReportBtn.OnTapped = func() { openInDefaultApp(summary.ReportPath) }
				openReportBtn.Enable()
			})
		}()
	})

	form := container.NewVBox(
		widget.NewLabel("Input folder"),
		container.NewBorder(nil, nil, nil, browseFolder(inEntry), inEntry),
		widget.NewLabel("Output folder"),
		container.NewBorder(nil, nil, nil, browseFolder(outEntry), outEntry),
		widget.NewLabel("ONNX model"),
		container.NewBorder(nil, nil, nil, browseFile(modelEntry), modelEntry),
		widget.NewLabel("onnxruntime library"),
		container.NewBorder(nil, nil, nil, browseFile(libEntry), libEntry),
		widget.NewLabel("Worker count"),
		workersEntry,
		container.NewGridWithColumns(2,
			container.NewVBox(widget.NewLabel("Move threshold (Explicit)"), highEntry),
			container.NewVBox(widget.NewLabel("Review threshold (Suggestive)"), lowEntry),
		),
		gpuCheck,
		dryRunCheck,
		useDefaultExcludesCheck,
		excludeEntry,
		runBtn,
		progressBar,
		status,
		openReportBtn,
	)

	w.SetContent(container.NewPadded(container.NewVScroll(form)))
	w.Resize(fyne.NewSize(520, 640))
	w.ShowAndRun()
}

func openInDefaultApp(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	_ = cmd.Start()
}
