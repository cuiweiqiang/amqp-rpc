# RabbitMQ RPC

[![GoDoc](https://godoc.org/github.com/bombsimon/amqp-rpc?status.svg)](https://godoc.org/github.com/bombsimon/amqp-rpc)
[![Build Status](https://travis-ci.org/bombsimon/amqp-rpc.svg?branch=master)](https://travis-ci.org/bombsimon/amqp-rpc)
[![Go Report Card](https://goreportcard.com/badge/github.com/bombsimon/amqp-rpc)](https://goreportcard.com/report/github.com/bombsimon/amqp-rpc)
[![Maintainability](https://api.codeclimate.com/v1/badges/77ecbf483dc76d4327a5/maintainability)](https://codeclimate.com/github/bombsimon/amqp-rpc/maintainability)
[![Test Coverage](https://api.codeclimate.com/v1/badges/77ecbf483dc76d4327a5/test_coverage)](https://codeclimate.com/github/bombsimon/amqp-rpc/test_coverage)
[![golangci](https://golangci.com/badges/github.com/bombsimon/amqp-rpc.svg)](https://golangci.com/r/github.com/bombsimon/amqp-rpc)

## Description

This is a framework to use [RabbitMQ](https://www.rabbitmq.com) with
[Go amqp](https://github.com/streadway/amqp) as RPC client/server setup. The
purpose of this framework is to implement a fully functional message queue setup
where a user can just plug in handlers on the server(s) and use the client to
communicate between them. This is suitable for many micro service architectures.

We assume that the user has some knowledge about RabbitMQ and preferrably the Go
package since a few of the types are exposed to the end user. However, for
simple setups there's no need for understanding of any of this.

## Components

This framework consists of a client and a server with related beloning
components such as request, responses, connections and other handy types.

### Server

The server is designed to be a *plug-and-play* component with handlers attached
to endpoints for amqp messages. All you need to do to start consuming
messages published to `routing_key` looks like this:

```go
s := NewServer("amqp://guest:guest@localhost:5672")

s.Bind(DirectBinding("routing_key", func(c context.Context, rw *ResponseWriter d *amqp.Delivery) {
    // Print what the body and header was
    fmt.Println(d.Body, d.Headers)

    // Add a response to the client
    fmt.Fprint(rw, "Handled")
}))

s.ListenAndServe()
```

This example will use the default exchange for direct bindings (`direct`) and
use the routing key provided as queue name. It's also possible to specify other
kind of exchanges such as topic or fanout by using the `HandlerBinding` type.
This package already supports direct, fanout, topic and header.

```go
s := NewServer("amqp://guest:guest@localhost:5672")

s.Bind(DirectBinding("routing_key", handleFunc))
s.Bind(FanoutBinding("fanout-exchange", handleFunc))
s.Bind(TopicBinding("queue-name", "routing_key.#", handleFunc))
s.Bind(HeadersBinding("queue-name", amqp.Table{"x-match": "all", "foo": "bar"}, handleFunc))

customBinding := HandlerBinding{
    QueueName:    "oh-sweet-queue",
    ExchangeName: "my-exchange",
    ExchangeType: "direct",
    RoutingKey:   "my-key",
    BindHeaders:  amqp.Table{},
    Handler:      handleFunc,
}

s.Bind(customBinding)
```

#### Server middlewares

Middlewares can be hooked to both a specific handler and to the entire server to
be executed on all request no matter what endpoint. You can also chain
middlewares to execute them in a specific order or execute multiple ones for
specific use cases.

Inspired by the [http](https://godoc.org/net/http), the middleware is defined as
a function that takes a handler function as input and returns an identical
handler function.

```go
type ServerMiddlewareFunc func(next HandlerFunc) Handlerfunc
```

To execute the inner handler, call `next` with the correct arguments which is
a context, a response writer and an amqp.Delivery:

```go
func myMiddle(next HandlerFunc) HandlerFunc {
    // Preinitialization of middleware here.

    return func(ctx context.Context, rw *ResponseWriter d amqp.Delivery) {
        // Before handler execution here.

        // Execute the handler.
        next(ctx, rw, d)

        // After execution here.
    }
}

s := NewServer("amqp://guest:guest@localhost:5672")

// Add a middleware to specific handler.
s.Bind(DirectBinding("foobar", myMiddle(HandlerFunc)))

// Add multiple middlewares to specific handler.
s.Bind(
    DirectBinding(
        "foobar",
        ServerMiddlewareChain(
            myHandler,
            middlewareOne,
            middlewareTwo,
            middlewareThree,
        ),
    )
)

// Add middleware to all handlers on the server.
s.AddMiddleware(myMiddle)

s.ListenAndServe()
```

### Client

The clien is designed to look similar to the server in usage and be just as easy
to configure for your likings. One feature of the client is that it's build
around channels where all messages are mapped to unique correlation IDs. This
means that the server is non blocking and can handle multiple requests at once.

```go
c := NewClient("amqp://guest:guest@localhost:5672")

request := NewRequest().WithRoutingKey("my_endpoint").WithBody("My body").WithResponse(true)
response, err := c.Send(request)
if err != nil {
    logger.Warn("Something went wrong", err)
}

logger.Info(string(response.Body))
```

The client will not connect upon calling the constructor, instead this is made
the first time a connection is required, usually when calling `Send`. By doing
this you're able to chain multiple methods after calling new to modify the
client settings.

```go
c := NewClient("amqp://guest:guest@localhost:5672").
    WithTimeout(5000 * time.Milliseconds).
    WithDialConfig(dialConfig).
    WithTLS(cert).
    WithQueueDeclareSettings(qdSettings).
    WithConsumeSettings(cSettings).
    WithHeaders(amqp.Table{})

// Will not connect until this call.
c.Send(NewRequest().WithRoutingKey("queue_one"))
```

#### Sender

The client comes with a default implementation which is a complete send function
that connects to the message bus, publishes the message and if desired waits for
a reply. However, the sender is just anotehr `SendFunc` which can be overridden.
This is great for testing or in other ways mocking without the need implement an
interface. This library even comes with a test package which can return a client
of this type!

```go
customSendFunc := func(r *Request) (*amqp.Delivery, error) {
    fmt.Println("Will not connect or send")

    return &amqp.Delivery{}, nil
}

c := amqprpctest.NewTestClient(customSendFunc)
c.Send(NewRequest())
```

#### Request

To perform requests easily the client expects a `Request` type as input when
sending messages. This type holds all the information about which exchange,
headers, body, content type, routing key, timeout and if a reply is desired. If
a setting can be configured on both the client and the request (i.e. timeout or
middlewares), the configuration on the request has a higher priority.

This is an example of how to send a fanout request without waiting for
responses.

```go
c := NewClient("amqp://guest:guest@localhost:5672")
r := NewRequest().WithExchange("fanout-exchange").WithResponse(false)

_, err := c.Send(r)
```

Just like the server and client, your options is chainable.

```go
r := NewRequest().
    WithBody(`{"hello":"world"}`).
    WithContentType("application/json").
    WithContext(context.Background()).
    WithExchange("custom.exchange").
    WithHeaders(amqp.Headers{}).
    WithResponse(true).
    WithRoutingKey("routing-key").
    WithCorrelationID("my-unique-id").
    WithTimeout(5 * time.Second)
```

Or use the request as an io.Writer(), like the `ResponseWriter`.

```go
r := NewRequest()

encoder := json.NewEncoder(r)
encoder.Encode(serializableObject)
```

**Note**: If you request a response when sending to a fanout exchange the
response will be the first one respondend from any of the subscribers. There is
currently no way to accept multiple responses or responses in a specific order.

#### Client middlewares

Just like the server this framework is implementing support to be able to
easily plug code before and after a request is being sent with the client.

The middleware is defined as a function that takes a send function and returns a
send function. The client itself implements the root `SendFunc` that generates the
request and publishes it.

```go
type SendFunc func(r *Request) (*amqp.Delivery, error)
```

The `*amqp.Delivery` is the response from the server and potential
errors will be returned as well.

Just like the server you can choose to chain your custom methods to one or just
add them one by one with the add interface.

```go
func MySendMiddleware (next amqprpc.SendFunc) amqprpc.SendFunc {
    return func(r *amqprpc.Request) (*amqp.Delivery, error) {
        r.Publishing.Headers["foo"] = "bar"
        r.Publishing.AppId = "my-app"

        return next(r)
    }
}

c := NewClient(url).AddMiddleware(MySendMiddleware)
```

The client can also take middlewares for single requests with the exact same
interface.

```go
c := NewClient(url).AddMiddleware(MySendMiddleware)
r := NewRequest().WithRoutingKey("some.where").AddMiddleware(MyOtherMiddleware)

c.Send(r)
```

Since the request is more specific it's middlewares are executed **after** the
clients middlewares. This is so the request can override headers etc.

Se `examples/middleware` for more examples.

### Logger

You can specifiy an optional logger for amqp errors, unexpected behaviour etc.
By default only error logging is turned on and is logged via the log package's
standard logging.

But you can profide your own logging function for both error and debug on both
the `Client` and the `Server`.

```go
debugLogger := log.New(os.Stdout, "DEBUG - ", log.LstdFlags)
errorLogger := log.New(os.Stdout, "ERROR - ", log.LstdFlags)

server := NewServer(url)
server.WithErrorLogger(errorLogger.Printf)
server.WithDebugLogger(debugLogger.Printf)

client := NewClient(url)
client.WithErrorLogger(errorLogger.Printf)
client.WithDebugLogger(debugLogger.Printf)
```

This is perfect when for example using Logrus logger:

```go
logger := logrus.New()
logger.SetLevel(logrus.DebugLevel)

server := NewServer(url)
server.WithErrorLogger(logger.Warnf)
server.WithDebugLogger(logger.Debugf)

client := NewClient(url)
client.WithErrorLogger(logger.Errorf)
client.WithDebugLogger(logger.Debugf)
```

### Connection and TLS

As a part of the mantra to minimize implementation and handling of the actual
conections this framework implements a really easy way to use TLS for either the
server or the client bu just providing the path to CA, cert and key files. Under
the hood this part only loads the key pair and adds the TLS configuration to the
amqp configuration field.

```go
cert := Certificates{
    Cert: "/path/to/cert.pem",
    Key:  "/path/to/key.pem",
    CA:   "/path/to/cacert.pem",
}

// Now we can pass this to the server or client and connect with TLS.
uri := "amqps://guest:guest@localhost:5671"
dialConfig := amqp.Config{
    TLSClientConfig: cert.TLSConfig(),
}

s := NewServer(uri).WithDialConfig(dialConfig)
c := NewClient(uri).WithDialConfig(dialConfig)

s.ListenAndServe()
```

## Examples

There are a few examples included in the `examples` folder. For more information
about how to customize your setup, see the documentation (linked above).
