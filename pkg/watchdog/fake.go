package watchdog

import (
	"context"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	fakeTimeout = 1 * time.Second
)

// fakeWatchdogImpl provides the fake implementation of the watchdogImpl interface for tests
type fakeWatchdogImpl struct {
	IsStartSuccessful bool
}

type FakeDog struct {
	*synchronizedWatchdog
}

func NewFake(isStartSuccessful bool) *FakeDog {
	fakeWDImpl := &fakeWatchdogImpl{IsStartSuccessful: isStartSuccessful}
	syncedDog := newSynced(ctrl.Log.WithName("fake watchdog"), fakeWDImpl)
	//return syncedDog
	return &FakeDog{syncedDog}
}

func (f *fakeWatchdogImpl) start() (*time.Duration, error) {
	if !f.IsStartSuccessful {
		return nil, errors.New("fakeWatchdogImpl crash on start")
	}
	t := fakeTimeout
	return &t, nil
}

func (f *fakeWatchdogImpl) feed() error {
	return nil
}

func (f *fakeWatchdogImpl) disarm() error {
	return nil
}

func (fd *FakeDog) Reset() {
	swd := fd.synchronizedWatchdog
	swd.mutex.Lock()
	defer swd.mutex.Unlock()
	if swd.status != Armed {
		swd.stop()
		swd.status = Armed
		fd.startFeeding()
	}
}

func (fd *FakeDog) startFeeding() {
	swd := fd.synchronizedWatchdog
	feedCtx, cancel := context.WithCancel(context.Background())
	swd.stop = cancel
	// feed until stopped
	go wait.NonSlidingUntilWithContext(feedCtx, func(feedCtx context.Context) {
		swd.mutex.Lock()
		defer swd.mutex.Unlock()
		// prevent feeding of a disarmed watchdog in case the context isn't cancelled yet
		if swd.status != Armed {
			return
		}
		if err := swd.impl.feed(); err != nil {
			swd.log.Error(err, "failed to feed watchdog!")
		} else {
			swd.lastFoodTime = time.Now()
		}
	}, swd.timeout/3)
}
