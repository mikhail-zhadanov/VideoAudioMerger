# Video Audio Merger
A small windows GUI application to merge MP4 video and M4A audio files from two URLs.

## Build
1. Install Go from [here](https://golang.org/dl/).
2. Clone the repository.
3. Download ffmpeg.exe from [here](https://ffmpeg.org/download.html) and place it in the root directory of the repository.
4. Download upx.exe from [here](https://upx.github.io/) and place it in the root directory of the repository.
3. Run the following commands in the terminal:
```bash
go build -ldflags "-H=windowsgui -s -w" -o VideoAudioMerger.exe
upx --brute VideoAudioMerger.exe
```
