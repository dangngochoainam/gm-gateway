package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	productsvcv1 "github.com/dangngochoainam/gm-core/application/product/v1/service"
	"github.com/dangngochoainam/gm-gateway/configs"
	"github.com/dangngochoainam/gm-gateway/internal/diregistry"
	"github.com/dangngochoainam/gm-gateway/internal/gatewaymux"
	"github.com/dangngochoainam/gm-gateway/internal/health"
	"github.com/dangngochoainam/gm-gateway/internal/middleware"
	"github.com/dangngochoainam/gopkg/grpcclienthelper"
	"github.com/dangngochoainam/gopkg/loghelper"
	"github.com/rs/cors"
)

func main() {

	diregistry.BuildContainer()

	config := diregistry.GetDependency[*configs.Config]()

	err := loghelper.InitZapLogger(config.Env, "gm-gateway")
	if err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}
	defer loghelper.Logger.Sync()

	// Cancelled on SIGINT/SIGTERM, which starts the drain below.
	shutdownCtx, stopListeningForSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopListeningForSignals()

	// gRPC client connections to the downstream domain services.
	// The gateway serves no gRPC of its own — it only translates HTTP to gRPC.
	productConn, err := grpcclienthelper.NewClientConn(&grpcclienthelper.GrpcClientConfig{
		Host: config.GrpcServices.Product.Host,
		Port: config.GrpcServices.Product.Port,
	})
	if err != nil {
		log.Fatalln("Failed to create gm-product client:", err)
	}
	defer productConn.Close()

	// Routes generated from google.api.http annotations are forwarded to the
	// service that owns them. One RegisterXHandler call per downstream service.
	mux := gatewaymux.New()
	err = productsvcv1.RegisterProductServiceHandler(context.Background(), mux, productConn)
	if err != nil {
		log.Fatalln("Failed to register gateway:", err)
	}

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   config.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowCredentials: true,
		AllowedHeaders:   []string{"Content-Type", "Authorization", "Cookie", middleware.RequestIDHeader},
		ExposedHeaders:   []string{"Set-Cookie", middleware.RequestIDHeader},
		// Enable Debugging for testing, consider disabling in production
		// Debug: true,
	}).Handler(mux)

	// Outermost first. RequestID leads so everything below it logs one
	// correlation ID; AccessLog sits above Recovery so a recovered panic is
	// still logged as a 500; Timeout sits innermost so its deadline covers only
	// the downstream call.
	handler := middleware.Chain(corsHandler,
		middleware.RequestID,
		middleware.AccessLog,
		middleware.Recovery,
		middleware.StripIdentityHeaders,
		middleware.MaxBodyBytes(config.Server.MaxRequestBodyBytes),
		middleware.Timeout(config.Server.RequestTimeout),
	)

	readiness := health.NewReadiness()

	root := http.NewServeMux()
	health.Register(root, readiness)
	root.Handle("/", handler)

	httpServer := &http.Server{
		Handler:           root,
		ReadHeaderTimeout: config.Server.ReadHeaderTimeout,
		ReadTimeout:       config.Server.ReadTimeout,
		WriteTimeout:      config.Server.WriteTimeout,
		IdleTimeout:       config.Server.IdleTimeout,
		MaxHeaderBytes:    config.Server.MaxHeaderBytes,
	}

	listenerHttp, err := net.Listen("tcp", fmt.Sprintf(":%s", config.HttpPort))
	if err != nil {
		log.Panic("Http Server: Can not create listener - err: ", err)
	}

	go func() {
		err := httpServer.Serve(listenerHttp)
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	// Routes are registered, so the pod may take traffic.
	// TODO: readiness does not reflect downstream health. grpc.NewClient dials
	// lazily, so /readyz reports 200 while gm-product is down and real requests
	// get 503 UNAVAILABLE. See docs/gateway-criteria.md, 1.11.
	readiness.SetReady()
	loghelper.Logger.Infow("gateway running", "address", listenerHttp.Addr().String())

	<-shutdownCtx.Done()
	drainAndShutdown(httpServer, readiness, config.Server)
}

// drainAndShutdown fails readiness first, then waits before shutting the server
// down. Kubernetes removes the pod from the Service endpoint list asynchronously,
// so closing the listener immediately on SIGTERM drops requests that are still
// being routed here. Keep terminationGracePeriodSeconds above
// shutdown_drain_delay + shutdown_timeout.
func drainAndShutdown(httpServer *http.Server, readiness *health.Readiness, cfg configs.ServerConfig) {
	readiness.SetNotReady()
	loghelper.Logger.Infow("shutdown signal received, draining", "drain_delay", cfg.ShutdownDrainDelay.String())
	time.Sleep(cfg.ShutdownDrainDelay)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		loghelper.Logger.Errorw("graceful shutdown failed", "error", err)
		return
	}
	loghelper.Logger.Info("graceful shutdown complete")
}
