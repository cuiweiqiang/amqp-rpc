package amqprpc

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
)

const (
	bindingsTestURL = "amqp://guest:guest@localhost:5672"
)

func TestFanout(t *testing.T) {
	var (
		assert      = assert.New(t)
		timesCalled int64
		called      = make(chan struct{})
	)

	fanoutHandler := func(ctx context.Context, rw *ResponseWriter, d amqp.Delivery) {
		atomic.AddInt64(&timesCalled, 1)
		called <- struct{}{}
	}

	for range make([]struct{}, 3) {
		s := NewServer(bindingsTestURL)
		s.Bind(FanoutBinding("fanout-exchange", fanoutHandler))

		stop := startAndWait(s)
		defer stop()
	}

	c := NewClient(bindingsTestURL)
	_, err := c.Send(NewRequest().WithExchange("fanout-exchange").WithResponse(false))

	// Ensure all handlers have added to the timesCalled variable.
	for range make([]int, 3) {
		select {
		case <-called:
			// Great!
		case <-time.After(time.Second):
			t.Error("fanoutHandler was not called")
		}

	}

	assert.Nil(err, "no errors occurred")
	assert.Equal(atomic.LoadInt64(&timesCalled), int64(3), "endpoint called 3 times")
}

func TestTopic(t *testing.T) {
	wasCalled := map[string]chan string{
		"foo.#": make(chan string),
		"foo.*": make(chan string),
		"baz.*": make(chan string),
	}

	s := NewServer(bindingsTestURL)
	c := NewClient(bindingsTestURL)

	s.Bind(TopicBinding("", "foo.#", func(ctx context.Context, rw *ResponseWriter, d amqp.Delivery) {
		wasCalled["foo.#"] <- string(d.Body)
	}))
	s.Bind(TopicBinding("", "foo.*", func(ctx context.Context, rw *ResponseWriter, d amqp.Delivery) {
		wasCalled["foo.*"] <- string(d.Body)
	}))
	s.Bind(TopicBinding("", "baz.*", func(ctx context.Context, rw *ResponseWriter, d amqp.Delivery) {
		wasCalled["baz.*"] <- string(d.Body)
	}))

	stop := startAndWait(s)
	defer stop()

	cases := []struct {
		request string
		called  map[string]bool
	}{
		{
			request: "foo.bar",
			called:  map[string]bool{"foo.#": true, "foo.*": true, "baz.*": false},
		},
		{
			request: "foo.bar.baz",
			called:  map[string]bool{"foo.#": true, "foo.*": false, "baz.*": false},
		},
		{
			request: "baz.bar.foo",
			called:  map[string]bool{"foo.#": false, "foo.*": false, "baz.*": false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.request, func(t *testing.T) {
			_, err := c.Send(NewRequest().WithRoutingKey(tc.request).WithBody(tc.request).WithExchange("amq.topic").WithResponse(false))
			assert.Nil(t, err, nil, "no errors sending topic request")

			for key, expectCalled := range tc.called {
				select {
				case body := <-wasCalled[key]:
					if !expectCalled {
						t.Errorf("%s WAS called on %s with body %s", key, tc.request, body)
					}
					assert.Equal(t, tc.request, body, "correct request body")
				case <-time.After(10 * time.Millisecond):
					if expectCalled {
						t.Errorf("%s NOT called on %s", key, tc.request)
					}
				}
			}
		})
	}
}

func TestHeaders(t *testing.T) {
	s := NewServer(bindingsTestURL)
	c := NewClient(bindingsTestURL)

	handler := func(ctx context.Context, rw *ResponseWriter, d amqp.Delivery) {
		fmt.Fprintf(rw, "Hello, world")
	}

	h := amqp.Table{
		"x-match": "all",
		"foo":     "bar",
	}

	s.Bind(HeadersBinding("", h, handler))

	stop := startAndWait(s)
	defer stop()

	// Ensure 'somewhere.*' matches 'somewhere.there'.
	response, err := c.Send(NewRequest().WithExchange("amq.match").WithHeaders(amqp.Table{"foo": "bar"}))

	assert.Nil(t, err, "no errors occurred")
	assert.Equal(t, []byte("Hello, world"), response.Body, "correct request body")
}
