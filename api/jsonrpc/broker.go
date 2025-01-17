package jsonrpc

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
	"github.com/sirupsen/logrus"

	"github.com/axiomesh/axiom-ledger/internal/coreapi/api"
	"github.com/axiomesh/axiom-ledger/pkg/loggers"
	"github.com/axiomesh/axiom-ledger/pkg/ratelimiter"
	"github.com/axiomesh/axiom-ledger/pkg/repo"
)

type ChainBrokerService struct {
	rep *repo.Repo

	// genesis     *repo.Genesis
	api api.CoreAPI

	server      *rpc.Server
	wsServer    *rpc.Server
	logger      logrus.FieldLogger
	rateLimiter *ratelimiter.JRateLimiter

	ctx    context.Context
	cancel context.CancelFunc
}

func NewChainBrokerService(coreAPI api.CoreAPI, rep *repo.Repo) (*ChainBrokerService, error) {
	logger := loggers.Logger(loggers.API)

	jLimiter := rep.Config.JsonRPC.Limiter
	rateLimiter, err := ratelimiter.NewJRateLimiterWithQuantum(jLimiter.Interval.ToDuration(), jLimiter.Capacity, jLimiter.Quantum)
	if err != nil {
		return nil, fmt.Errorf("create rate limiter failed: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cbs := &ChainBrokerService{
		logger:      logger,
		rep:         rep,
		api:         coreAPI,
		ctx:         ctx,
		cancel:      cancel,
		rateLimiter: rateLimiter,
	}

	if err := cbs.init(); err != nil {
		cancel()
		return nil, fmt.Errorf("init chain broker service failed: %w", err)
	}

	if err := cbs.initWS(); err != nil {
		cancel()
		return nil, fmt.Errorf("init chain broker websocket service failed: %w", err)
	}

	return cbs, nil
}

func (cbs *ChainBrokerService) init() error {
	cbs.server = rpc.NewServer()

	apis, err := GetAPIs(cbs.rep, cbs.api, cbs.logger)
	if err != nil {
		return fmt.Errorf("get apis failed: %w", err)
	}

	// Register all the APIs exposed by the namespace services
	for _, api := range apis {
		if err := cbs.server.RegisterName(api.Namespace, api.Service); err != nil {
			return fmt.Errorf("register name %s for service %v failed: %w", api.Namespace, api.Service, err)
		}
	}

	return nil
}

func (cbs *ChainBrokerService) initWS() error {
	cbs.wsServer = rpc.NewServer()

	apis, err := GetAPIs(cbs.rep, cbs.api, cbs.logger)
	if err != nil {
		return fmt.Errorf("get apis failed: %w", err)
	}

	// Register all the APIs exposed by the namespace services
	for _, api := range apis {
		if err := cbs.wsServer.RegisterName(api.Namespace, api.Service); err != nil {
			return fmt.Errorf("register name %s for service %v failed: %w", api.Namespace, api.Service, err)
		}
	}

	return nil
}

func (cbs *ChainBrokerService) Start() error {
	router := mux.NewRouter()
	handler := cbs.tokenBucketMiddleware(cbs.server)
	router.Handle("/", handler)

	wsRouter := mux.NewRouter()
	wsHandler := node.NewWSHandlerStack(cbs.wsServer.WebsocketHandler([]string{"*"}), []byte(""))
	wsRouter.Handle("/", wsHandler)

	go func() {
		cbs.logger.WithFields(logrus.Fields{
			"port": cbs.rep.Config.Port.JsonRpc,
		}).Info("JSON-RPC service started")

		if err := http.ListenAndServe(fmt.Sprintf(":%d", cbs.rep.Config.Port.JsonRpc), cors.Default().Handler(router)); err != nil {
			cbs.logger.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Errorf("Failed to start JSON_RPC service: %s", err.Error())
			return
		}
	}()

	go func() {
		cbs.logger.WithFields(logrus.Fields{
			"port": cbs.rep.Config.Port.WebSocket,
		}).Info("Websocket service started")

		if err := http.ListenAndServe(fmt.Sprintf(":%d", cbs.rep.Config.Port.WebSocket), cors.Default().Handler(wsRouter)); err != nil {
			cbs.logger.WithFields(logrus.Fields{
				"error": err.Error(),
			}).Errorf("Failed to start websocket service: %s", err.Error())
			return
		}
	}()

	return nil
}

func (cbs *ChainBrokerService) Stop() error {
	cbs.cancel()

	cbs.server.Stop()

	cbs.logger.Info("JSON-RPC service stopped")

	return nil
}

func (cbs *ChainBrokerService) tokenBucketMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait until a token is obtained before processing the request
		if cbs.rateLimiter.JLimit() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Continue processing to the next middleware or request handler
		next.ServeHTTP(w, r)
	})
}
