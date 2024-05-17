package main

import (
	"bytes"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type WriteInfo struct {
	filename    string
	timestamp   int64
	changesSize int
	fileSize    int
	offset      int64
}

var (
	ch = make(chan WriteInfo, 1)
)

// Watch one or more files, but instead of watching the File directly it watches
// the parent directory. This solves various issues where files are frequently
// renamed, such as editors saving them.
func File(files ...string) {
	if len(files) < 1 {
		exit("must specify at least one File to Watch")
	}

	// Create a new watcher.
	w, err := fsnotify.NewWatcher()
	if err != nil {
		exit("creating a new watcher: %s", err)
	}
	defer w.Close()

	// Start listening for events.
	go fileLoop(w, files)

	// Add all files from the commandline.
	for _, p := range files {
		st, err := os.Lstat(p)
		if err != nil {
			exit("%s", err)
		}

		if st.IsDir() {
			exit("%q is a directory, not a File", p)
		}

		// Watch the directory, not the File itself.
		err = w.Add(filepath.Dir(p))
		if err != nil {
			exit("%q: %s", p, err)
		}
	}

	var totalSize int
	printTime("ready; press ^C to exit")
	for info := range ch {
		printTime("%s %d %d modsize:%d offset:%d", info.filename, info.fileSize, info.timestamp, info.changesSize, info.offset)
		totalSize += info.changesSize
		if info.changesSize == 0 {
			continue
		}

		r, err := os.Open(info.filename)
		if err != nil {
			log.Fatal(err)
		}

		err = r.Sync()
		if err != nil {
			log.Fatal(err)
		}

		size := info.changesSize
		offset := info.offset

		data := make([]byte, size)

		_, err = r.ReadAt(data, offset)
		if err != nil && err != io.EOF {
			log.Fatal(err)
		}

		yaLeido := make([]byte, totalSize-info.changesSize)
		_, err = r.ReadAt(yaLeido, 0)

		if err != nil && err != io.EOF {
			log.Fatal(err)
		}

		f, err := os.OpenFile("/home/sunamed/fsnotify/output/new_test.tiff", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			log.Fatal(err)
		}

		currentBytes, err := os.ReadFile("/home/sunamed/fsnotify/output/new_test.tiff")
		if err != nil && err != io.EOF {
			log.Fatal(err)
		}

		if !bytes.Equal(yaLeido, currentBytes) {
			differences := findDifferences(currentBytes, yaLeido)
			fmt.Println("Differences at indices:", differences)
			fmt.Println("Differences at indices 2:", findDifferences(yaLeido, currentBytes))
			reWrite := make([]byte, len(differences))
			_, err = r.ReadAt(reWrite, int64(differences[0]))
			if err != nil && err != io.EOF {
				log.Fatal(err)
			}

			_, err = f.Seek(int64(differences[0]), 0)
			if err != nil {
				log.Fatal(err)
			}

			_, err = f.Write(reWrite)
			if err != nil {
				log.Fatal(err)
			}

			//log.Fatal("Data mismatch between source and destination")
		}

		r.Close()
		_, err = f.Seek(info.offset, 0)
		if err != nil {
			log.Fatal(err)
		}

		_, err = f.Write(data)
		if err != nil {
			log.Fatal(err)
		}

		/*_, err = f.WriteAt(data, info.offset)
		if err != nil {
			log.Fatal(err)
		}*/
		f.Close()

		printTime("Total Write :%d kb", totalSize)
		if totalSize == 10530212 {
			srcBytes, err := os.ReadFile(info.filename)
			if err != nil && err != io.EOF {
				log.Fatal(err)
			}

			destBytes, err := os.ReadFile("/home/sunamed/fsnotify/output/new_test.tiff")
			if err != nil && err != io.EOF {
				log.Fatal(err)
			}

			if !bytes.Equal(srcBytes, destBytes) {
				differences := findDifferences(srcBytes, destBytes)
				fmt.Println("Differences at indices:", differences)
			}
		}
	}
	<-make(chan struct{}) // Block forever
}

func findDifferences(a, b []byte) []int {
	var indices []int

	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			indices = append(indices, i)
		}
	}

	if len(a) != len(b) {
		start := minLen
		end := len(a)
		if len(b) > len(a) {
			end = len(b)
		}
		for i := start; i < end; i++ {
			indices = append(indices, i)
		}
	}

	return indices
}

func fileLoop(w *fsnotify.Watcher, files []string) {
	i := 0

	var (
		lastFilesSizes = make(map[string]int)
		mu             sync.Mutex
	)

	for _, file := range files {
		lastFilesSizes[file] = 0
	}

	for {
		select {
		// Read from Errors.
		case err, ok := <-w.Errors:
			if !ok { // Channel was closed (i.e. Watcher.Close() was called).
				return
			}
			printTime("ERROR: %s", err)
		// Read from Events.
		case e, ok := <-w.Events:
			if !ok { // Channel was closed (i.e. Watcher.Close() was called).
				return
			}

			if !e.Has(fsnotify.Create) && !e.Has(fsnotify.Write) {
				continue
			}

			// Ignore files we're not interested in. Can use a
			// map[string]struct{} if you have a lot of files, but for just a
			// few files simply looping over a slice is faster.
			var found bool
			for _, f := range files {
				if f == e.Name {
					found = true
				}
			}
			if !found {
				continue
			}

			f, err := os.Open(e.Name)
			if err != nil {
				continue
			}

			var size int
			var offset int64
			var modified int64
			if info, err := f.Stat(); err == nil {
				size64 := info.Size()
				if int64(int(size64)) == size64 {
					size = int(size64)
				}

				modified = info.ModTime().Unix()
			}
			//size++

			mu.Lock()
			lastFileSize := lastFilesSizes[e.Name]
			mu.Unlock()
			offset = int64(size - (size - lastFileSize))

			f.Close()
			// Just print the event nicely aligned, and keep track how many
			// events we've seen.
			i++

			//printTime("%3d %s %d %d modsize:%d offset:%d", i, e, size, modified, size-lastSize, offset)
			ch <- WriteInfo{filename: e.Name, timestamp: modified, offset: offset, changesSize: size - lastFileSize, fileSize: size}

			mu.Lock()
			lastFilesSizes[e.Name] = size
			mu.Unlock()
		}
	}
}
