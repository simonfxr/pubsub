package pubsub

import (
	"reflect"
	"sync"
	a "sync/atomic"
	"unsafe"
)

// A simple string used to describe a topic
type Topic = string

// The event to be published
type Event = interface{}

// The event Bus.
// Should never be copied!
type Bus struct {
	subs sync.Map
}

type rawSub = unsafe.Pointer

type atomicSub struct {
	raw rawSub
}

var nilSub = atomicSub{rawSub((*Subscription)(nil))}

type invoker interface {
	onEvent(sub *Subscription, arg reflect.Value, event [1]reflect.Value)
	onUnsubscribed(sub *Subscription, arg reflect.Value)
}

type rawflag = uintptr

type flags struct {
	argarity     int8 // read only
	unsubscribed bool // idempotent writes
	oflag        int8 // read only
}

// A handle to a bus subscription
type Subscription struct {
	invoker invoker         // read only
	arg     reflect.Value   // read only
	sl      *subscriberList // read only
	next    atomicSub       // unlocked reads, locked writes
	flags   rawflag         // unlocked reads/writes
	noCopy  noCopy
}

type subscriberList struct {
	subs       [2]atomicSub // [0]: repeated subscribers, [1]: once subscribers
	sync.Mutex              // writer lock
	topic      Topic        // needed for unsubscribe
}

// Create a new event bus with a default table size of 512.
func NewBus() *Bus {
	return NewSizedBus(512)
}

// Create a new event bus with a specific hash table size
func NewSizedBus(size int) *Bus {
	return &Bus{}
}

// Publish an event under a specific topic.
func (b *Bus) Publish(t Topic, event Event) {
	sl_, ok := b.subs.Load(t)
	if !ok {
		return
	}

	sl := sl_.(*subscriberList)
	del := false

	sl.Lock()
	rep := sl.subs[0].loadRelaxed()
	once := sl.subs[1].loadRelaxed()
	if once != nil {
		sl.subs[1].storeRelaxed(nil)
		// delete if we consumed all remaining subscriptions
		del = rep == nil
	}
	sl.Unlock()

	if del {
		b.subs.Delete(t)
	}

	args := [1]reflect.Value{reflect.ValueOf(event)}

	head := once
	for head != nil {
		head.setUnsubscribed(head.fl())
		head.invoker.onEvent(head, head.arg, args)
		head.invoker.onUnsubscribed(head, head.arg)
		// non atomic load: no writer can have access to it
		head = head.next.loadRelaxed()
	}

	head = rep
	for head != nil {
		head.invoker.onEvent(head, head.arg, args)
		// atomic load: there may be a concurrent writer in Unsubscribe()
		head = head.next.load()
	}

}

func (b *Bus) subscribe(
	t Topic, invoker invoker, arg reflect.Value, once bool,
) *Subscription {
	oflag := boolToInt(once)
	flags := flags{oflag: int8(oflag), unsubscribed: false, argarity: -1}
	sub := &Subscription{invoker: invoker, arg: arg, next: nilSub}
	argt := arg.Type()
	if argt.Kind() == reflect.Func {
		flags.argarity = int8(argt.NumIn())
	}
	sub.flags = flags.encode()
	for {
		nsl := &subscriberList{topic: t}
		nsl.subs[oflag].storeRelaxed(sub)
		sub.sl = nsl
		sl_, loaded := b.subs.LoadOrStore(t, nsl)
		if !loaded {
			return sub
		}
		sub.sl = sl_.(*subscriberList)
		done := false
		sub.sl.Lock()
		if !sub.sl.subs[0].isNil() || !sub.sl.subs[1].isNil() {
			done = true
			sub.next = sub.sl.subs[oflag]
			sub.sl.subs[oflag].storeRelaxed(sub)
		}
		sub.sl.Unlock()
		if done {
			return sub
		}
	}
}

// Unsubscribe from the bus
// Returns true if the subscription was active and is now unsubscribed
// Returns false if the subscription was already unsubscribed
func (b *Bus) Unsubscribe(sub *Subscription) bool {

	// relaxed read is okay
	flags := sub.fl()
	if flags.unsubscribed {
		return false
	}

	sl_, ok := b.subs.Load(sub.sl.topic)
	if !ok {
		return false
	}
	sl := sl_.(*subscriberList)
	if sl != sub.sl {
		return false
	}

	sl.Lock()
	// search for the to be removed subscription
	// could use double links to avoid the iteration
	prev := (*Subscription)(nil)
	head := sub.sl.subs[flags.oflag].loadRelaxed()
	del := false
	for head != nil {
		next := head.next.loadRelaxed()
		if head == sub {
			if prev == nil {
				sub.sl.subs[flags.oflag].storeRelaxed(next)
				del = sl.subs[0].isNil() && sl.subs[1].isNil()
			} else {
				prev.next.store(next)
			}
			break
		}
		prev, head = head, next
	}
	sl.Unlock()
	if head == nil {
		return false
	}
	if del {
		b.subs.Delete(sl.topic)
	}
	sub.setUnsubscribed(flags)
	sub.invoker.onUnsubscribed(sub, sub.arg)
	return true
}

func (s *Subscription) fl() flags { return decodeFlags(s.flags) }

func (s *Subscription) setUnsubscribed(fl flags) {
	fl.unsubscribed = true
	a.StoreUintptr(&s.flags, fl.encode())
}

// Test if this subscription is currently subscribed
func (s *Subscription) Subscribed() bool {
	return !decodeFlags(a.LoadUintptr(&s.flags)).unsubscribed
}

// Get the number of arguments of the callback attached to this subscription.
// If the subscription does not have a callback return -1.
func (s *Subscription) CallbackArity() int { return int(s.fl().argarity) }

func (f flags) encode() rawflag {
	return (rawflag(f.oflag) << 16) |
		rawflag(boolToInt(f.unsubscribed)<<8) |
		rawflag(uint8(f.argarity))
}

func decodeFlags(x rawflag) flags {
	return flags{
		argarity:     int8(x),
		unsubscribed: int8(x>>8) != 0,
		oflag:        int8(x >> 16),
	}
}

func (p *atomicSub) load() *Subscription { return (*Subscription)(a.LoadPointer(&p.raw)) }

func (p *atomicSub) loadRelaxed() *Subscription { return (*Subscription)(p.raw) }

func (p *atomicSub) store(x *Subscription) { a.StorePointer(&p.raw, rawSub(x)) }

func (p *atomicSub) storeRelaxed(x *Subscription) { p.raw = rawSub(x) }

func (p *atomicSub) isNil() bool { return p.loadRelaxed() == nil }

func boolToInt(x bool) int {
	if x {
		return 1
	}
	return 0
}

type noCopy struct{}

// copying will trigger go vet copy locks warning
func (_ *noCopy) Lock()   {}
func (_ *noCopy) Unlock() {}
