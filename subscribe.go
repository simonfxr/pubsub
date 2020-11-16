package pubsub

import (
	"reflect"
	a "sync/atomic"
)

// A callback func to consume an event
// For an event of type T the following signatures are allowed:
//
// 1. func()
// Ignore the event if you not interested in the event payload itself
//
// 2. func(event T)
// Process the event and its payload
//
// 3. func(event T, subscription *Subscription)
// Additionally get a pointer to the subscription of this handler,
// useful to unsubscribe itself if a certain condition is met
type Callback = interface{}

// A writable chan:
// chan T or chan<- T
type WritableChan = interface{}

// whether a channel subscription should close the target channel when unsubscribing
type CloseFlag bool

// Close the channel when unsubscribing
const CloseOnUnsubscribe CloseFlag = true

// Don't close the channel when unsubscribing
const KeepOpen CloseFlag = false

// Subscribe to a topic with a callback, the callback may be invoked multiple
// times. The callback will executed in the context of the publishing goroutine,
// and thus should not block or panic. Callback invocations can overlap if there
// are overlapping publications to the topic. Use one of the Async variants if
// any of this is undesirable.
func (b *Bus) Subscribe(t Topic, cb Callback) *Subscription {
	return b.subscribe(t, directInvoker, checkCb(cb), false)
}

// Subscribe to a topic with a callback, the handler may be called at most once.
// See Subscribe(...) for more details.
func (b *Bus) SubscribeOnce(t Topic, cb Callback) *Subscription {
	return b.subscribe(t, directInvoker, checkCb(cb), true)
}

// Subscribe to a topic with a callback.
// The callback may be invoked multiple times, each invocation in its own goroutine.
// Callback invocations can overlap independent of overlapping publishers.
func (b *Bus) SubscribeAsync(t Topic, cb Callback) *Subscription {
	return b.subscribe(t, asyncInvoker, checkCb(cb), false)
}

// Subscribe to a topic with a callback.
// The callback will be invoked in its own goroutine and at most once.
func (b *Bus) SubscribeAsyncOnce(t Topic, cb Callback) *Subscription {
	return b.subscribe(t, asyncInvoker, checkCb(cb), true)
}

// Subscribe to a topic with a channel. On publish the event will be written to
// the channel, if the channel is full the event will be dropped.
func (b *Bus) SubscribeChan(t Topic, ch WritableChan, close CloseFlag) *Subscription {
	invoker := chanInvoker
	if close {
		invoker = &invokeChanWithClose{}
	}
	return b.subscribe(t, invoker, reflect.ValueOf(ch), false)
}

// Subscribe to a topic with a channel. On publish the event will be written to
// the channel, if the channel is full the event will be dropped.
// At most one event will be written to the channel.
//
// close: If close is true the channel will be closed when unsubscribing.
func (b *Bus) SubscribeChanOnce(t Topic, ch WritableChan, close CloseFlag) *Subscription {
	invoker := chanInvoker
	if close {
		invoker = &invokeChanWithClose{}
	}
	return b.subscribe(t, invoker, checkWritableChan(ch), true)
}

// Subscribe to a topic with a channel and wait until either an event is
// published or the abort channel is written to / closed. abort can be a nil
// channel in which case this method blocks indefinitely until an event is
// published.
// Returns the event and a flag indicating whether an event was received.
func (b *Bus) SubscribeOnceWait(t Topic, abort <-chan struct{}) (event Event, ok bool) {
	evchan := make(chan interface{}, 1)
	sub := b.SubscribeChanOnce(t, evchan, true)
	if abort == nil {
		ev, ok := <-evchan
		return ev, ok
	}
	select {
	case <-abort:
		b.Unsubscribe(sub)
		return nil, false
	case ev, ok := <-evchan:
		return ev, ok
	}
}

func checkCb(cb Callback) reflect.Value {
	v := reflect.ValueOf(cb)
	if v.Kind() != reflect.Func || v.Type().NumIn() > 2 {
		panic("Invalid callback type: " + v.Type().String())
	}
	return v
}

func checkWritableChan(ch WritableChan) reflect.Value {
	v := reflect.ValueOf(ch)
	if v.Kind() != reflect.Chan || v.Type().ChanDir() == reflect.RecvDir {
		panic("Invalid channel type: " + v.Type().String())
	}
	return v
}

var directInvoker invoker = invokeDirect{}

type invokeDirect struct{}

func (_ invokeDirect) onEvent(sub *Subscription, f reflect.Value, ev [1]reflect.Value) {
	switch sub.CallbackArity() {
	case 1:
		f.Call(ev[:])
	case 2:
		args := [2]reflect.Value{ev[0], reflect.ValueOf(sub)}
		f.Call(args[:])
	case 0:
		args := [0]reflect.Value{}
		f.Call(args[:])
	}
}

func (_ invokeDirect) onUnsubscribed(*Subscription, reflect.Value) {}

var asyncInvoker invoker = invokeAsync{}

type invokeAsync struct{}

func (_ invokeAsync) onEvent(sub *Subscription, f reflect.Value, ev [1]reflect.Value) {
	go directInvoker.onEvent(sub, f, ev)
}

func (_ invokeAsync) onUnsubscribed(*Subscription, reflect.Value) {}

var chanInvoker invoker = invokeChan{}

type invokeChan struct{}

func (_ invokeChan) onEvent(sub *Subscription, ch reflect.Value, ev [1]reflect.Value) {
	ch.TrySend(ev[0])
}

func (_ invokeChan) onUnsubscribed(*Subscription, reflect.Value) {}

type invokeChanWithClose struct {
	// unsubscribe can overlap a publish, we have to ensure that chan is not
	// used after closing it so we use a simple spin lock to accomplish that.
	//
	// states:
	// 0: unused
	// 1: used by a sender
	// -1: closed
	lock int32
}

func (i *invokeChanWithClose) onEvent(sub *Subscription, ch reflect.Value, ev [1]reflect.Value) {
	for {
		if a.CompareAndSwapInt32(&i.lock, 0, 1) {
			break
		}
		n := a.LoadInt32(&i.lock)
		if n < 0 {
			return
		}
	}
	defer a.StoreInt32(&i.lock, 0)
	ch.TrySend(ev[0])
}

func (i *invokeChanWithClose) onUnsubscribed(sub *Subscription, ch reflect.Value) {
	for {
		if a.CompareAndSwapInt32(&i.lock, 0, -1) {
			ch.Close()
			return
		}
		n := a.LoadInt32(&i.lock)
		if n < 0 {
			return
		}
	}
}
