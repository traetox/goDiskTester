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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type historyItem struct {
	Path    string
	TS      time.Time
	Results diskTestResult
}

type history struct {
	sync.Mutex
	pth    string
	fout   *os.File
	jenc   *json.Encoder
	items  []historyItem
	active map[string]bool
}

func newHistory(pth string) (h *history, err error) {
	var fio *os.File
	if fio, err = os.OpenFile(pth, os.O_RDWR|os.O_CREATE, 0640); err != nil {
		err = fmt.Errorf("Failed to open %v - %v", *stateDb, err)
		return
	}
	jdec := json.NewDecoder(fio)
	var items []historyItem
	for {
		var item historyItem
		if err = jdec.Decode(&item); err != nil {
			if err == io.EOF {
				err = nil
				break // this is just fine
			}
			// some other error
			fio.Close()
			err = fmt.Errorf("failed to decode history item %v", err)
			return
		}
		items = append(items, item)
	}
	jenc := json.NewEncoder(fio)
	jenc.SetIndent("", "\t")
	h = &history{
		pth:    pth,
		fout:   fio,
		jenc:   jenc,
		items:  items,
		active: map[string]bool{},
	}
	return
}

func (h *history) Close() error {
	h.Lock()
	defer h.Unlock()
	return h.fout.Close()
}

func (h *history) MarkActive(pth string) {
	h.Lock()
	defer h.Unlock()
	h.active[pth] = true
	return
}

func (h *history) Check(pth string) bool {
	h.Lock()
	defer h.Unlock()
	if _, ok := h.active[pth]; ok {
		return true
	}
	for _, v := range h.items {
		if v.Path == pth {
			return true
		}
	}
	return false
}

func (h *history) Add(pth string, ts time.Time, res diskTestResult) (err error) {
	if len(pth) == 0 {
		err = fmt.Errorf("Path required")
		return
	}
	h.Lock()
	defer h.Unlock()
	//delete from active
	delete(h.active, pth)
	err = h.jenc.Encode(historyItem{
		Path:    pth,
		TS:      ts,
		Results: res,
	})
	return
}
