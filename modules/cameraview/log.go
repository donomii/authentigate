package main

import (
	"errors"
	"log"
	"net/url"
)

func cameraErrorCause(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return urlError.Err
	}
	return err
}

func logCameraOpening(index int, name string) {
	log.Printf("streamManager: opening upstream for camera %d (%q)", index, name)
}

func logCameraClosed(index int, name string) {
	log.Printf("streamManager: upstream closed for camera %d (%q)", index, name)
}

func logCameraOpenFailed(index int, name string, err error) {
	log.Printf("streamManager: could not open camera %d (%q): %v", index, name, err)
}

func logCameraReadFailed(index int, name string, err error) {
	log.Printf("streamManager: read ended for camera %d (%q): %v", index, name, err)
}

func logCameraPageRequest(id string) {
	log.Printf("cameraview: user=%q", id)
}

func logCameraStreamRequest(id string) {
	log.Printf("cameraview stream: user=%q", id)
}

func logCameraViewerDeadlineFailed(index int, name string, err error) {
	log.Printf("streamManager: could not interrupt disconnected viewer for camera %d (%q): %v", index, name, err)
}
