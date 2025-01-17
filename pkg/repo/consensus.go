package repo

import (
	"path"
	"time"

	"github.com/pkg/errors"

	"github.com/axiomesh/axiom-kit/fileutil"
)

type ReceiveMsgLimiter struct {
	Enable bool  `mapstructure:"enable" toml:"enable"`
	Limit  int64 `mapstructure:"limit" toml:"limit"`
	Burst  int64 `mapstructure:"burst" toml:"burst"`
}

type ConsensusSync struct {
	FetchConcurrencyLimit int `mapstructure:"fetch_concurrency_limit" toml:"fetch_concurrency_limit"`
	FetchSizeLimit        int `mapstructure:"fetch_size_limit" toml:"fetch_size_limit"`
}

type ConsensusConfig struct {
	TimedGenBlock TimedGenBlock     `mapstructure:"timed_gen_block" toml:"timed_gen_block"`
	Limit         ReceiveMsgLimiter `mapstructure:"limit" toml:"limit"`
	Sync          ConsensusSync     `mapstructure:"sync" toml:"sync"`
	TxPool        TxPool            `mapstructure:"tx_pool" toml:"tx_pool"`
	TxCache       TxCache           `mapstructure:"tx_cache" toml:"tx_cache"`
	Rbft          RBFT              `mapstructure:"rbft" toml:"rbft"`
	Solo          Solo              `mapstructure:"solo" toml:"solo"`
}

type TimedGenBlock struct {
	NoTxBatchTimeout Duration `mapstructure:"no_tx_batch_timeout" toml:"no_tx_batch_timeout"`
}

type TxPool struct {
	PoolSize            uint64   `mapstructure:"pool_size" toml:"pool_size"`
	BatchTimeout        Duration `mapstructure:"batch_timeout" toml:"batch_timeout"`
	ToleranceTime       Duration `mapstructure:"tolerance_time" toml:"tolerance_time"`
	ToleranceRemoveTime Duration `mapstructure:"tolerance_remove_time" toml:"tolerance_remove_time"`
	ToleranceNonceGap   uint64   `mapstructure:"tolerance_nonce_gap" toml:"tolerance_nonce_gap"`
}

type TxCache struct {
	SetSize    int      `mapstructure:"set_size" toml:"set_size"`
	SetTimeout Duration `mapstructure:"set_timeout" toml:"set_timeout"`
}

type RBFT struct {
	EnableMultiPipes                              bool        `mapstructure:"enable_multi_pipes" toml:"enable_multi_pipes"`
	EnableMetrics                                 bool        `mapstructure:"enable_metrics" toml:"enable_metrics"`
	CheckInterval                                 Duration    `mapstructure:"check_interval" toml:"check_interval"`
	MinimumNumberOfBatchesToRetainAfterCheckpoint uint64      `mapstructure:"minimum_number_of_batches_to_retain_after_checkpoint" toml:"minimum_number_of_batches_to_retain_after_checkpoint"`
	Timeout                                       RBFTTimeout `mapstructure:"timeout" toml:"timeout"`
}

type RBFTTimeout struct {
	SyncState        Duration `mapstructure:"sync_state" toml:"sync_state"`
	SyncInterval     Duration `mapstructure:"sync_interval" toml:"sync_interval"`
	Recovery         Duration `mapstructure:"recovery" toml:"recovery"`
	FirstRequest     Duration `mapstructure:"first_request" toml:"first_request"`
	Request          Duration `mapstructure:"request" toml:"request"`
	NullRequest      Duration `mapstructure:"null_request" toml:"null_request"`
	ViewChange       Duration `mapstructure:"viewchange" toml:"viewchange"`
	ResendViewChange Duration `mapstructure:"resend_viewchange" toml:"resend_viewchange"`
	CleanViewChange  Duration `mapstructure:"clean_viewchange" toml:"clean_viewchange"`
	Update           Duration `mapstructure:"update" toml:"update"`
}

type Solo struct {
	CheckpointPeriod uint64 `mapstructure:"checkpoint_period" toml:"checkpoint_period"`
}

func DefaultConsensusConfig() *ConsensusConfig {
	if testNetConsensusConfigBuilder, ok := TestNetConsensusConfigBuilderMap[BuildNet]; ok {
		return testNetConsensusConfigBuilder()
	}

	// nolint
	return &ConsensusConfig{
		TimedGenBlock: TimedGenBlock{
			NoTxBatchTimeout: Duration(2 * time.Second),
		},
		Limit: ReceiveMsgLimiter{
			Enable: false,
			Limit:  10000,
			Burst:  10000,
		},
		Sync: ConsensusSync{
			FetchConcurrencyLimit: 50,
			FetchSizeLimit:        1000,
		},
		TxPool: TxPool{
			PoolSize:            50000,
			BatchTimeout:        Duration(500 * time.Millisecond),
			ToleranceTime:       Duration(5 * time.Minute),
			ToleranceRemoveTime: Duration(15 * time.Minute),
			ToleranceNonceGap:   1000,
		},
		TxCache: TxCache{
			SetSize:    50,
			SetTimeout: Duration(100 * time.Millisecond),
		},
		Rbft: RBFT{
			EnableMultiPipes: false,
			EnableMetrics:    true,
			CheckInterval:    Duration(3 * time.Minute),
			MinimumNumberOfBatchesToRetainAfterCheckpoint: 10,
			Timeout: RBFTTimeout{
				SyncState:        Duration(3 * time.Second),
				SyncInterval:     Duration(1 * time.Minute),
				Recovery:         Duration(15 * time.Second),
				FirstRequest:     Duration(30 * time.Second),
				Request:          Duration(6 * time.Second),
				NullRequest:      Duration(9 * time.Second),
				ViewChange:       Duration(8 * time.Second),
				ResendViewChange: Duration(10 * time.Second),
				CleanViewChange:  Duration(60 * time.Second),
				Update:           Duration(4 * time.Second),
			},
		},
		Solo: Solo{
			CheckpointPeriod: 10,
		},
	}
}

func LoadConsensusConfig(repoRoot string) (*ConsensusConfig, error) {
	cfg, err := func() (*ConsensusConfig, error) {
		cfg := DefaultConsensusConfig()
		cfgPath := path.Join(repoRoot, consensusCfgFileName)
		existConfig := fileutil.Exist(cfgPath)
		if !existConfig {
			if err := writeConfigWithEnv(cfgPath, cfg); err != nil {
				return nil, errors.Wrap(err, "failed to build default consensus config")
			}
		} else {
			if err := readConfigFromFile(cfgPath, cfg); err != nil {
				return nil, err
			}
		}
		return cfg, nil
	}()
	if err != nil {
		return nil, errors.Wrap(err, "failed to load network config")
	}
	return cfg, nil
}
