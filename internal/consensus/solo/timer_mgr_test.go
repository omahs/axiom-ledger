package solo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/axiomesh/axiom-kit/log"
)

func TestBatchTimer_StartBatchTimer(t *testing.T) {
	logger := log.NewWithModule("consensus")
	eventCh := make(chan consensusEvent, maxChanSize)
	tm := NewTimerManager(eventCh, logger)

	var (
		batchEnd     time.Duration
		noTxBatchEnd time.Duration
	)
	batchTimeout := 500 * time.Millisecond
	noTxBatchTimeout := 1 * time.Second
	tm.newTimer(Batch, 0)
	tm.newTimer(NoTxBatch, noTxBatchTimeout)

	start := time.Now()

	batchEventCh := make(chan struct{})
	noTxBatchEventCh := make(chan struct{})
	defer func() {
		close(eventCh)
		close(batchEventCh)
		close(noTxBatchEventCh)
	}()
	tm.startTimer(Batch)
	tm.startTimer(NoTxBatch)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-eventCh:
				switch e.(batchTimeoutEvent) {
				case Batch:
					batchEnd = time.Since(start)
					batchEventCh <- struct{}{}
				case NoTxBatch:
					noTxBatchEnd = time.Since(start)
					noTxBatchEventCh <- struct{}{}
				}
			}
		}
	}()
	<-batchEventCh
	<-noTxBatchEventCh
	cancel()
	require.True(t, batchEnd >= batchTimeout)
	require.True(t, noTxBatchEnd >= noTxBatchTimeout)
}

func TestBatchTimer_StopBatchTimer(t *testing.T) {
	logger := log.NewWithModule("consensus")
	eventCh := make(chan consensusEvent, maxChanSize)
	batchTimer := NewTimerManager(eventCh, logger)
	batchTimeout := 200 * time.Millisecond
	noTxBatchTimeout := 2 * time.Second
	batchTimer.newTimer(Batch, batchTimeout)
	batchTimer.newTimer(NoTxBatch, 0)
	ch := make(chan struct{})
	defer func() {
		close(eventCh)
		close(ch)
	}()
	start := time.Now()
	batchTimer.startTimer(Batch)
	batchTimer.startTimer(NoTxBatch)
	go func() {
		select {
		case e := <-eventCh:
			switch e.(batchTimeoutEvent) {
			case Batch:
				require.Fail(t, "batch timer should not be triggered")
			case NoTxBatch:
				require.True(t, time.Since(start) >= noTxBatchTimeout)
				ch <- struct{}{}
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	batchTimer.stopTimer(Batch)
	require.False(t, batchTimer.isTimerActive(Batch))
	require.True(t, batchTimer.isTimerActive(NoTxBatch))
	<-ch
	batchTimer.Stop()
	require.False(t, batchTimer.isTimerActive(NoTxBatch))
}
