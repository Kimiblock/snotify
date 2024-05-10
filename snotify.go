package main

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
)

var (
	mu       sync.Mutex
	lastLine string
)

func monitorDbus(path, member string) {
	for {
		cmd := exec.Command("dbus-monitor", fmt.Sprintf("path='%s',member='%s'", path, member))
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			fmt.Println("Error creating stdout pipe:", err)
			return
		}

		if err := cmd.Start(); err != nil {
			fmt.Println("Error starting dbus-monitor:", err)
			return
		}

		buf := make([]byte, 512) // Limit the line length to 512 bytes

		for {
			n, err := stdout.Read(buf)
			if err != nil {
				fmt.Println("Error reading from dbus-monitor:", err)
				break
			}

			line := string(buf[:n])

			// Lock the mutex to safely update lastLine
			mu.Lock()
			lastLine = line
			mu.Unlock()
		}

		// Wait for the command to finish
		if err := cmd.Wait(); err != nil {
			fmt.Println("dbus-monitor exited with an error:", err)
		}

		// Sleep for a while before restarting dbus-monitor
		time.Sleep(5 * time.Second)
	}
}

func playSoundOnNewLine() {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Lock the mutex to safely read lastLine
		mu.Lock()
		currentLine := lastLine
		mu.Unlock()

		if currentLine != "" {
			fmt.Println("Received a Notify or AddNotification event. Playing sound...")
			playSound()

			// Clear lastLine to prevent repeated execution
			mu.Lock()
			lastLine = ""
			mu.Unlock()
		}
	}
}

func playSound() {
	soundCmd := exec.Command("mpv", "--audio-client-name=snotify", "--no-video", "--no-terminal", "--no-audio-display", "--no-config", "--really-quiet", "--volume=80", "/opt/snotify/message.ogg")
	if err := soundCmd.Start(); err != nil {
		fmt.Println("Error playing sound:", err)
		return
	}

	if err := soundCmd.Wait(); err != nil {
		fmt.Println("Error waiting for sound:", err)
	}
}

func main() {
	go monitorDbus("/org/freedesktop/Notifications", "Notify") // Start monitoring dbus for the first path and member
	go monitorDbus("/org/gtk/Notifications", "AddNotification") // Start monitoring dbus for the second path and member
	go playSoundOnNewLine()                           // Start checking for new lines and playing sound in a goroutine

	// The program will run indefinitely without waiting for Enter key input
	select {}
}
