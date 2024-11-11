package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	url2 "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Embed ffmpeg.exe using Go 1.16's embed package
//
//go:embed ffmpeg.exe
var ffmpegData []byte

func main() {
	myApp := app.NewWithID("com.zhadanov.videoaudiomerger")
	myWindow := myApp.NewWindow("Video Audio Merger")
	myWindow.Resize(fyne.NewSize(600, 600))
	myWindow.SetIcon(theme.MediaVideoIcon())

	// Video URL entry
	videoURL := widget.NewEntry()
	videoURL.ActionItem = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		videoURL.SetText("")
	})
	videoURL.SetPlaceHolder("Enter the video URL")
	if url := myApp.Preferences().String("LastVideoURL"); url != "" {
		videoURL.SetText(url)
	}

	// Audio URL entry
	audioURL := widget.NewEntry()
	audioURL.ActionItem = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		audioURL.SetText("")
	})

	audioURL.SetPlaceHolder("Enter the audio URL")
	if url := myApp.Preferences().String("LastAudioURL"); url != "" {
		audioURL.SetText(url)
	}
	audioURL.OnChanged = func(text string) {
		myApp.Preferences().SetString("LastAudioURL", text)
	}

	// Output file name entry
	outputFileName := widget.NewEntry()
	outputFileName.SetPlaceHolder("Enter the output file name (e.g., output.mp4)")
	if url := myApp.Preferences().String("LastOutputFileName"); url != "" {
		outputFileName.SetText(url)
	}
	outputFileName.ActionItem = widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		outputFileName.SetText("")
	})
	outputFileName.OnChanged = func(text string) {
		myApp.Preferences().SetString("LastOutputFileName", text)
	}

	// Auto-derive file name from video URL
	videoURL.OnChanged = func(text string) {
		myApp.Preferences().SetString("LastVideoURL", text)
		fileName := deriveFileNameFromURL(text)
		if fileName != "" {
			outputFileName.SetText(fileName)
		}
	}

	// Output logs entry (set to read-only)
	output := widget.NewMultiLineEntry()
	output.SetPlaceHolder("Output logs will appear here")
	output.Wrapping = fyne.TextWrapWord
	output.SetMinRowsVisible(10)
	output.Disable() // Make it read-only

	// Single progress bar and label
	progressLabel := widget.NewLabel("")
	progressBar := widget.NewProgressBar()
	progressBar.Min = 0
	progressBar.Max = 1
	progressBar.SetValue(0)

	// progressBar.Hide()
	// progressLabel.Hide()

	// Directory selector components
	lastDirPath := myApp.Preferences().StringWithFallback("LastDirectory", "")
	destDirLabel := widget.NewLabel("No directory selected")
	if lastDirPath != "" {
		destDirLabel.SetText(lastDirPath)
	}

	destDirButton := widget.NewButton("Select Destination Directory", func() {
		dirDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, myWindow)
				return
			}
			if uri != nil {
				destDirPath := uri.Path()
				destDirLabel.SetText(destDirPath)
				myApp.Preferences().SetString("LastDirectory", destDirPath)
			}
		}, myWindow)
		dirDialog.Resize(fyne.NewSize(600, 600))

		var initialURI fyne.ListableURI
		if lastDirPath != "" {
			// Convert lastDirPath to a ListableURI if available
			uri := storage.NewFileURI(lastDirPath)
			if _, ok := uri.(fyne.ListableURI); ok {
				dirDialog.SetLocation(initialURI)
			}
		}

		dirDialog.Show()
	})

	startBtn := widget.NewButton("Start", func() {
		output.SetText("")
		go func() {
			videoURLText := videoURL.Text
			audioURLText := audioURL.Text
			destDirPath := destDirLabel.Text
			outputFileNameText := outputFileName.Text

			if videoURLText == "" || audioURLText == "" || destDirPath == "" || outputFileNameText == "" {
				appendOutput(output, "Please fill in all fields.")
				return
			}

			if _, err := os.Stat(destDirPath); os.IsNotExist(err) {
				appendOutput(output, "Destination directory does not exist.")
				return
			}

			videoDest := filepath.Join(destDirPath, "video.mp4")
			audioDest := filepath.Join(destDirPath, "audio.mp4")
			finalDest := filepath.Join(destDirPath, outputFileNameText)

			// Download video
			appendOutput(output, "Downloading video...")
			showProgressBar(progressBar, progressLabel, "Downloading video...")
			if err := downloadFile(videoDest, videoURLText, output, progressBar); err != nil {
				appendOutput(output, fmt.Sprintf("Failed to download video: %v", err))
				hideProgressBar(progressBar, progressLabel)
				return
			}
			appendOutput(output, "Video download completed.")
			hideProgressBar(progressBar, progressLabel)

			// Download audio
			appendOutput(output, "Downloading audio...")
			showProgressBar(progressBar, progressLabel, "Downloading audio...")
			if err := downloadFile(audioDest, audioURLText, output, progressBar); err != nil {
				appendOutput(output, fmt.Sprintf("Failed to download audio: %v", err))
				hideProgressBar(progressBar, progressLabel)
				return
			}
			appendOutput(output, "Audio download completed.")
			hideProgressBar(progressBar, progressLabel)

			// Merge video and audio
			appendOutput(output, "Merging video and audio...")
			showProgressBar(progressBar, progressLabel, "Merging video and audio...")
			ffmpegPath, err := getFFmpegPath()
			if err != nil {
				appendOutput(output, fmt.Sprintf("Failed to get ffmpeg path: %v", err))
				hideProgressBar(progressBar, progressLabel)
				return
			}

			if err := mergeVideoAndAudio(ffmpegPath, videoDest, audioDest, finalDest, output, progressBar); err != nil {
				appendOutput(output, fmt.Sprintf("Failed to merge video and audio: %v", err))
				hideProgressBar(progressBar, progressLabel)
				return
			}
			appendOutput(output, "Merging completed.")
			hideProgressBar(progressBar, progressLabel)

			// Clean up temporary files
			os.Remove(videoDest)
			os.Remove(audioDest)
			os.Remove(ffmpegPath)

			appendOutput(output, fmt.Sprintf("Process completed. The final video is located at %s", finalDest))
		}()
	})

	content := container.NewVBox(
		widget.NewLabel("Video URL:"),
		videoURL,
		widget.NewLabel("Audio URL:"),
		audioURL,
		widget.NewLabel("Destination Directory:"),
		destDirButton,
		destDirLabel,
		widget.NewLabel("Output File Name:"),
		outputFileName,
		startBtn,
		progressLabel,
		progressBar,
		output,
	)

	myWindow.SetContent(content)
	myWindow.ShowAndRun()
}

func appendOutput(output *widget.Entry, text string) {
	output.SetText(output.Text + text + "\n")
}

func showProgressBar(progressBar *widget.ProgressBar, progressLabel *widget.Label, labelText string) {
	progressBar.SetValue(0)
	progressBar.Show()
	progressLabel.SetText(labelText)
	progressLabel.Show()
}

func hideProgressBar(progressBar *widget.ProgressBar, progressLabel *widget.Label) {
	//progressBar.Hide()
	//progressLabel.Hide()
	progressLabel.SetText("Done!")
}

func downloadFile(filepath string, url string, output *widget.Entry, progressBar *widget.ProgressBar) error {
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	totalSize := resp.ContentLength
	if totalSize <= 0 {
		return fmt.Errorf("unable to determine file size")
	}

	progressReader := &ProgressReader{
		Reader: resp.Body,
		Total:  totalSize,
		UpdateProgress: func(progress float64) {
			progressBar.SetValue(progress)
		},
	}

	_, err = io.Copy(out, progressReader)
	if err != nil {
		return err
	}

	return nil
}

type ProgressReader struct {
	Reader         io.Reader
	Total          int64
	Current        int64
	UpdateProgress func(progress float64)
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	if n > 0 {
		pr.Current += int64(n)
		progress := float64(pr.Current) / float64(pr.Total)
		if progress > 1.0 {
			progress = 1.0
		}
		pr.UpdateProgress(progress)
	}
	return n, err
}

func getFFmpegPath() (string, error) {
	ffmpegName := "ffmpeg.exe"
	if runtime.GOOS != "windows" {
		ffmpegName = "ffmpeg"
	}

	tempDir := os.TempDir()
	ffmpegPath := filepath.Join(tempDir, ffmpegName)

	if _, err := os.Stat(ffmpegPath); os.IsNotExist(err) {
		// Write the embedded ffmpegData to temp directory
		err = os.WriteFile(ffmpegPath, ffmpegData, 0755)
		if err != nil {
			return "", err
		}
	}

	return ffmpegPath, nil
}

func mergeVideoAndAudio(ffmpegPath, videoPath, audioPath, outputPath string, output *widget.Entry, progressBar *widget.ProgressBar) error {
	// Prepare the ffmpeg command with progress output
	cmd := exec.Command(ffmpegPath, "-i", videoPath, "-i", audioPath, "-c", "copy",
		"-map", "0:v:0", "-map", "1:a:0", "-y", outputPath, "-progress", "pipe:1")

	// Create a pipe to capture stdout
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %v", err)
	}

	// Start the ffmpeg process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %v", err)
	}

	// Get total duration in milliseconds
	totalDurationMs, err := getVideoDurationInMs(videoPath)
	if err != nil {
		fmt.Printf("Warning: Unable to get video duration: %v\n", err)
		totalDurationMs = 0
	}

	// Read stdout output in a separate goroutine
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "out_time_ms=") {
				outTimeMsStr := strings.TrimPrefix(line, "out_time_ms=")
				outTimeMs, err := strconv.ParseInt(outTimeMsStr, 10, 64)
				if err == nil && totalDurationMs > 0 {
					progress := float64(outTimeMs) / float64(totalDurationMs)
					if progress > 1.0 {
						progress = 1.0
					}
					progressBar.SetValue(progress)
				}
			}
		}
	}()

	// Wait for the ffmpeg process to finish
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ffmpeg error: %v", err)
	}

	return nil
}

func getVideoDurationInMs(videoPath string) (int64, error) {
	ffmpegPath, err := getFFmpegPath()
	if err != nil {
		return 0, err
	}

	cmd := exec.Command(ffmpegPath, "-i", videoPath)
	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(err.Error(), "exit status 1") {
		return 0, err
	}

	durationRegex := regexp.MustCompile(`Duration: (\d{2}):(\d{2}):(\d{2}).(\d{2})`)
	matches := durationRegex.FindStringSubmatch(string(output))
	if matches == nil {
		return 0, fmt.Errorf("unable to parse duration")
	}

	hours, _ := strconv.ParseInt(matches[1], 10, 64)
	minutes, _ := strconv.ParseInt(matches[2], 10, 64)
	seconds, _ := strconv.ParseInt(matches[3], 10, 64)
	centiseconds, _ := strconv.ParseInt(matches[4], 10, 64)

	totalMs := ((hours*3600 + minutes*60 + seconds) * 1000) + (centiseconds * 10)
	return totalMs, nil
}

func deriveFileNameFromURL(urlStr string) string {
	// Parse the URL
	parsedURL, err := url2.Parse(urlStr)
	if err != nil {
		return ""
	}
	// Get the path segment
	pathSegments := strings.Split(parsedURL.Path, "/")
	if len(pathSegments) > 0 {
		lastSegment := pathSegments[len(pathSegments)-1]
		// Remove query parameters if any
		fileName := strings.Split(lastSegment, "?")[0]
		// Ensure the file has a .mp4 extension
		if !strings.HasSuffix(fileName, ".mp4") {
			fileName += ".mp4"
		}
		return fileName
	}
	return ""
}
