[![GoDoc](https://godoc.org/github.com/simonfxr/pubsub?status.svg)](https://godoc.org/github.com/simonfxr/pubsub)

# Pubsub - A fast concurrent in memory publish subscribe event bus for golang

Pubsub is an in memory event bus, its goals are to be simple yet powerful
intended to be used in highly concurrent environments. While there are already
quite a few different implementations in go (see below), all proved to be not a
good fit in one way or another.

The implementations tries to be scalable by building on a [lock free
hashtable](https://github.com/cornelk/hashmap) and a custom scalable list of
subscriptions. To avoid dead looks and interference across unrelated topics no
locks will be held, this also allows callbacks to unsubscribe them selves.

# Examples

```go

bus := pubsub.NewBus()

// subscribe with callback
sub := bus.Subscribe("topic", func(ev MyEvent) {
   ...
})

// publish
bus.Publish("topic", MyEvent{})

// unsubscribe
bus.Unsubscribe(sub)

// subscribe for at most one callback
sub := bus.SubscribeOnce("topic", func(ev MyEvent) {
   ...
})

// unsubscribe from handler if condition is met
sub := bus.SubscribeOnce("topic", func(ev MyEvent, sub *pubsub.Subscription) {
   if (dontcareanymore) {
      bus.Unsubscribe(sub)
   }
   ...
})

// subscribe by sending events to a channel
ch := make(chan MyEvent, 10)
sub := bus.SubscribeChan("topic", ch, pubsub.CloseOnUnsubscribe)
...
// Unsubscribe will also close the channel in a concurrency safe manner
bus.Unsubscribe(sub)


// Block until an event is received or context is cancelled
ctx context.Context = ...
ev, ok := bus.SubscribeOnceWait("topic", ctx.Done())
if ok { // event was received

}
```

# Similar projects

Here is a selection of some existing similar projects

- [EventBus](https://github.com/asaskevich/EventBus)
- [Bus](https://github.com/mustafaturan/bus/blob/master/bus.go)
