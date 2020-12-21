package pubsub

import (
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type Ev struct {
	x int
}

func TestSubscribe(t *testing.T) {
	assert := assert.New(t)

	bus := NewBus()

	var fired Ev
	sub := bus.Subscribe("topic", func(e Ev) {
		fired = e
	})

	assert.True(sub.Subscribed())
	assert.Equal(sub.CallbackArity(), 1)

	bus.Publish("topic", Ev{1})
	assert.Equal(fired, Ev{1})

	bus.Publish("topic", Ev{2})
	assert.Equal(fired, Ev{2})

	ok := bus.Unsubscribe(sub)
	assert.True(ok)

	bus.Publish("topic", Ev{3})
	assert.Equal(fired, Ev{2})

	ok = bus.Unsubscribe(sub)
	assert.False(ok)
}

func TestSubscribeOnce(t *testing.T) {
	assert := assert.New(t)

	bus := NewBus()

	var fired Ev
	sub := bus.SubscribeOnce("topic", func(e Ev) {
		fired = e
	})

	bus.Publish("topic", Ev{1})
	assert.Equal(fired, Ev{1})

	bus.Publish("topic", Ev{2})
	assert.Equal(fired, Ev{1})

	ok := bus.Unsubscribe(sub)
	assert.False(ok)

	bus.Publish("topic", Ev{3})
	assert.Equal(fired, Ev{1})

	ok = bus.Unsubscribe(sub)
	assert.False(ok)
}

func TestSubscribeOnce2(t *testing.T) {
	assert := assert.New(t)
	bus := NewBus()

	var fired1 Ev
	var fired2 Ev
	sub1 := bus.SubscribeOnce("topic", func(e Ev) {
		fired1 = e
	})

	sub2 := bus.SubscribeOnce("topic", func(e Ev) {
		fired2 = e
	})

	bus.Publish("topic", Ev{1})
	assert.Equal(fired1, Ev{1})
	assert.Equal(fired2, Ev{1})

	bus.Publish("topic", Ev{2})
	assert.Equal(fired1, Ev{1})
	assert.Equal(fired2, Ev{1})

	ok := bus.Unsubscribe(sub1)
	assert.False(ok)

	ok = bus.Unsubscribe(sub2)
	assert.False(ok)
}

func TestSubscribeMixed(t *testing.T) {
	assert := assert.New(t)
	bus := NewBus()

	var fired1 Ev
	var fired2 Ev
	sub1 := bus.Subscribe("topic", func(e Ev) {
		fired1 = e
	})

	sub2 := bus.SubscribeOnce("topic", func(e Ev) {
		fired2 = e
	})

	bus.Publish("topic", Ev{1})
	assert.Equal(fired1, Ev{1})
	assert.Equal(fired2, Ev{1})

	bus.Publish("topic", Ev{2})
	assert.Equal(fired1, Ev{2})
	assert.Equal(fired2, Ev{1})

	ok := bus.Unsubscribe(sub1)
	assert.True(ok)

	ok = bus.Unsubscribe(sub2)
	assert.False(ok)

	bus.Publish("topic", Ev{3})
	assert.Equal(fired1, Ev{2})
	assert.Equal(fired2, Ev{1})

	ok = bus.Unsubscribe(sub1)
	assert.False(ok)
}

func TestSubscribeChan(t *testing.T) {
	assert := assert.New(t)

	bus := NewBus()
	ch := make(chan Ev, 10)

	sub := bus.SubscribeChan("topic", ch, KeepOpen)

	bus.Publish("topic", Ev{1})
	ev, ok := <-ch
	assert.True(ok)
	assert.Equal(ev, Ev{1})

	bus.Publish("topic", Ev{2})
	ev, ok = <-ch
	assert.True(ok)
	assert.Equal(ev, Ev{2})

	ok = bus.Unsubscribe(sub)
	assert.True(ok)

	bus.Publish("topic", Ev{})
	select {
	case _, ok = <-ch:
		assert.True(false) // unreachable
	default:
		assert.True(true)
	}

	ok = bus.Unsubscribe(sub)
	assert.False(ok)
}

func TestSubscribeChanUnsubscribe(t *testing.T) {
	assert := assert.New(t)

	bus := NewBus()
	ch := make(chan Ev, 10)

	sub := bus.SubscribeChan("topic", ch, CloseOnUnsubscribe)

	assert.Equal(sub.CallbackArity(), -1)

	bus.Publish("topic", Ev{1})
	ev, ok := <-ch
	assert.True(ok)
	assert.Equal(ev, Ev{1})

	bus.Publish("topic", Ev{2})
	ev, ok = <-ch
	assert.True(ok)
	assert.Equal(ev, Ev{2})

	ok = bus.Unsubscribe(sub)
	assert.True(ok)

	bus.Publish("topic", Ev{})
	select {
	case _, ok = <-ch:
		assert.False(ok)
	default:
		assert.True(false) // unreachable
	}

	ok = bus.Unsubscribe(sub)
	assert.False(ok)
}

func TestSubscribeChanWait(t *testing.T) {
	assert := assert.New(t)

	bus := NewBus()

	done := make(chan struct{}, 1)

	var ev Ev
	var ok bool
	go func() {
		var ev_ interface{}
		ev_, ok = bus.SubscribeOnceWait("topic", nil)
		ev = ev_.(Ev)
		close(done)
	}()

	// hacky way of ensuring that the subscriber go routine is subscribed before
	// we do a publish
	runtime.Gosched()
	runtime.Gosched()
	time.Sleep(60 * time.Millisecond)
	runtime.Gosched()

	bus.Publish("topic", Ev{1})
	<-done

	assert.Equal(ev, Ev{1})
	assert.Equal(ok, true)
}
