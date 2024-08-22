/*************************************************************************
 *
 * Gravwell - "Consume all the things!"
 *
 * ________________________________________________________________________
 *
 * Copyright 2024 - All Rights Reserved
 * Gravwell Inc <legal@gravwell.io>
 * ________________________________________________________________________
 *
 * NOTICE:  This code is part of the Gravwell project and may not be shared,
 * published, sold, or otherwise distributed in any form without the express
 * written consent of its owners.
 *
 **************************************************************************/

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/utils"
)

const (
	workerCount = 64
)

var (
	stateDb = flag.String("db", "history.db", "File that maintains the history of the drive tester")
	rootDir = flag.String("root", "/dev/disk/by-id/", "Base directory to look for new drives")
	filter  = flag.String("filter", "", "path filters to limit which drives we operate on")
	wSize   = flag.Int64("write-size", 1, "Target write size in GB (half random, half contiguous)")
)

func main() {
	flag.Parse()
	if *stateDb == `` {
		log.Fatal("missing -db flag")
	} else if *rootDir == `` {
		log.Fatal("missing -root flag")
	} else if *filter == `` {
		log.Fatal("missing -filter flag")
	} else if *wSize <= 0 {
		log.Fatal("invalid write-size flag")
	}

	//check that the root is a directory
	if fi, err := os.Stat(*rootDir); err != nil {
		log.Fatalf("Failed to stat %s - %v\n", *rootDir, err)
	} else if fi.IsDir() == false {
		log.Fatalf("%s is not a directory\n", *rootDir)
	}

	globber := filepath.Join(*rootDir, *filter)
	if _, err := filepath.Glob(globber); err != nil {
		log.Fatalf("Glob pattern %v is bad\n", globber)
	}

	h, err := newHistory(*stateDb)
	if err != nil {
		log.Fatal("Failed to create a history object - %v\n", err)
	}
	defer h.Close()

	wg := sync.WaitGroup{}
	ch := make(chan string, 1024)
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker(&wg, ch, h)
	}

	if err := sampleDisks(globber, h, ch); err != nil {
		log.Fatalf("Failed to sample %s %v\n", *rootDir, err)
	}

	sampleTicker := time.NewTicker(2 * time.Second)
	defer sampleTicker.Stop()

	quitSig := utils.GetQuitChannel()
loop:
	for {
		select {
		case <-sampleTicker.C:
			if err := sampleDisks(globber, h, ch); err != nil {
				log.Fatalf("Failed to sample %s %v\n", *rootDir, err)
			}
		case <-quitSig:
			break loop
		}
	}
	close(ch)
	wg.Wait()
}

func sampleDisks(glob string, h *history, ch chan string) (err error) {
	var matches []string
	if matches, err = filepath.Glob(glob); err != nil {
		return
	}
	for _, v := range matches {
		if !h.Check(v) {
			h.MarkActive(v)
			ch <- v
		}
	}
	return
}

func worker(wg *sync.WaitGroup, ch chan string, h *history) {
	defer wg.Done()
	for v := range ch {
		fmt.Printf("%v Starting\n", v)
		if res, err := testDisk(v); err != nil {
			fmt.Printf("Failed test for %v %v\n", v, err)
		} else if err = h.Add(v, time.Now(), res); err != nil {
			fmt.Printf("Failed to record result for %v %v\n", v, err)
		} else {
			fmt.Printf("Tests completed successfully for %v\n", v)
		}
	}
}
