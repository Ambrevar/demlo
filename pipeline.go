// Copyright © 2013-2018 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"fmt"
	"os"
	"sync"
)

// Stage is the interface implemented by an object that can be added to a
// pipeline to process incoming FileRecords.
// Multiple stages of the same kind can be run in parallel.
// Init() and Close() are run once per goroutine.
type Stage interface {
	Init()
	Run(*FileRecord) error
	Close()
}

// Pipeline processes FileRecords through a sequence of Stages. A FileRecord is
// forwarded to the 'log' channel when a Stage Run() function returns an error,
// or to the 'output' channel otherwise.
//
// The pipeline design automates a few things:
// - It groups log messages by FileRecord; no manual flushing required.
// - It removes some parallelization boilerplate such as channel loops.
// - It makes it easy to change the number of goroutines allocated to the various stages.
type Pipeline struct {
	input  chan *FileRecord
	output chan *FileRecord
	log    chan *FileRecord
	logWg  sync.WaitGroup
}

// NewPipeline initializes a Pipeline with an input queue and a log queue.
// The Pipeline waits until its input channel is fed.
func NewPipeline(inputQueueSize, logQueueSize int) *Pipeline {
	var p Pipeline
	p.input = make(chan *FileRecord, inputQueueSize)
	p.output = p.input
	p.log = make(chan *FileRecord, logQueueSize)

	p.logWg.Add(1)
	go func() {
		for fr := range p.log {
			fmt.Fprint(os.Stderr, fr)
		}
		p.logWg.Done()
	}()

	// Return a reference so that the WaitGroup gets referenced properly.
	return &p
}

// Add appends a new stage to the Pipeline.
// The Pipeline 'input' does not change, but its 'output' gets forwarded to the
// new Stage. The Stage can be parallelized 'routineCount' times. 'routineCount'
// must be >0. 'NewStage' initializes a Stage structure for each goroutine. It
// allows for data separation between goroutines and keeps the Stage interface
// implicit.
func (p *Pipeline) Add(NewStage func() Stage, routineCount int) {
	if routineCount <= 0 {
		return
	}
	var wg sync.WaitGroup

	// The output queue is the size of the number of producing goroutines. It
	// ensures that routines are not blocking each other.
	out := make(chan *FileRecord, routineCount)

	wg.Add(routineCount)
	for i := 0; i < routineCount; i++ {
		go func(input <-chan *FileRecord) {
			s := NewStage()
			s.Init()
			for fr := range input {
				err := s.Run(fr)
				if err != nil {
					p.log <- fr
					continue
				}
				out <- fr
			}
			s.Close()
			wg.Done()
		}(p.output)
	}

	// Change output channel after all the routines have been set up to read from
	// the former output channel.
	p.output = out

	// Close channel when all routines are done.
	go func() {
		wg.Wait()
		close(out)
	}()
}

// Close the Pipeline to finish logging.
// Call it once the input has been fully produced and the output fully consumed.
func (p *Pipeline) Close() {
	close(p.log)
	p.logWg.Wait()
}
