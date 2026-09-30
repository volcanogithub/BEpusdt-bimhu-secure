package utils

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

func TestB22EmptyAPIKeyInterceptors(t *testing.T) {
	called := false
	err := tronGridApiKeyUnaryInterceptor(nil)(context.Background(), "test", nil, nil, nil,
		func(context.Context, string, interface{}, interface{}, *grpc.ClientConn, ...grpc.CallOption) error {
			called = true
			return nil
		})
	if err != nil || !called {
		t.Fatal("empty API key list interrupted RPC")
	}
	called = false
	_, err = tronGridApiKeyStreamInterceptor(nil)(context.Background(), nil, nil, "test",
		func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
			called = true
			return nil, nil
		})
	if err != nil || !called {
		t.Fatal("empty API key list interrupted stream RPC")
	}
}
