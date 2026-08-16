// Package otel 提供 OpenTelemetry 追踪初始化
package otel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Config 描述 kbot 的 OTLP traces 导出配置。Endpoint 必须是完整 URL；
// Langfuse self-hosted 示例：http://langfuse-web:3000/api/public/otel/v1/traces。
type Config struct {
	Endpoint       string
	Headers        string
	ServiceName    string
	ServiceVersion string
	Environment    string
	SampleRatio    float64
}

// MustInit 初始化OpenTelemetry，失败时panic
func MustInit(ctx context.Context, cfg Config) func() {
	cleanup, err := Init(ctx, cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to init otel: %v", err))
	}
	return cleanup
}

// Init 初始化OpenTelemetry追踪
func Init(ctx context.Context, cfg Config) (func(), error) {
	if cfg.Endpoint == "" {
		// 如果没有配置endpoint，使用no-op provider
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return func() {}, nil
	}

	// 创建资源
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "kbot"
	}
	serviceVersion := cfg.ServiceVersion
	if serviceVersion == "" {
		serviceVersion = "dev"
	}
	environment := cfg.Environment
	if environment == "" {
		environment = "dev"
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
			attribute.String("deployment.environment", environment),
			attribute.String("langfuse.environment", environment),
			attribute.String("langfuse.release", serviceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	// 创建OTLP HTTP导出器
	headers, err := parseHeaders(cfg.Headers)
	if err != nil {
		return nil, err
	}
	exporterOptions := []otlptracehttp.Option{
		// WithEndpointURL 保留 scheme 与 /api/public/otel/v1/traces 路径；
		// WithEndpoint 只接受 host:port，会让 Langfuse 完整 URL 失效。
		otlptracehttp.WithEndpointURL(cfg.Endpoint),
		otlptracehttp.WithTimeout(10 * time.Second),
	}
	if len(headers) > 0 {
		exporterOptions = append(exporterOptions, otlptracehttp.WithHeaders(headers))
	}
	exporter, err := otlptracehttp.New(ctx, exporterOptions...)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	// 创建批处理span处理器
	bsp := sdktrace.NewBatchSpanProcessor(exporter)

	// 创建tracer provider
	ratio := cfg.SampleRatio
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// 设置全局tracer provider
	otel.SetTracerProvider(tracerProvider)

	// 设置全局propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// 返回清理函数
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracerProvider.Shutdown(ctx)
	}, nil
}

// parseHeaders 解析 OTEL_EXPORTER_OTLP_HEADERS 风格的 k=v,k2=v2。
// SplitN 保留 Basic Auth base64 尾部的 '='。
func parseHeaders(raw string) (map[string]string, error) {
	headers := map[string]string{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("invalid OTLP header %q: want key=value", item)
		}
		headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return headers, nil
}
