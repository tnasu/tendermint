package mempool

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	abciclient "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/types"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func setupTxMempool(ctx context.Context, t testing.TB,
	height int64, size, cacheSize int, options ...TxMempoolOption) *TxMempool {
	t.Helper()

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	cfg, err := config.ResetTestRoot(strings.ReplaceAll(t.Name(), "/", "|"))
	require.NoError(t, err)
	cfg.Mempool = config.DefaultMempoolConfig()
	cfg.LogLevel = "info"
	logger, err := log.NewDefaultLogger(cfg.LogFormat, cfg.LogLevel)
	appConnMem, err := abciclient.NewLocalCreator(kvstore.NewApplication())(logger)
	require.NoError(t, err)
	require.NoError(t, appConnMem.Start(ctx))

	t.Cleanup(func() {
		os.RemoveAll(cfg.RootDir)
		cancel()
		appConnMem.Wait()
	})

	if size > -1 {
		cfg.Mempool.Size = size
	}
	if cacheSize > -1 {
		cfg.Mempool.CacheSize = cacheSize
	}
	return NewTxMempool(logger.With("test", t.Name()), cfg.Mempool, appConnMem, height, options...)
}

func TestTxMempoolWithCacheSizeZero(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	height := int64(1)
	txmp := setupTxMempool(ctx, t, 1, 3, 0)
	receiveTx(ctx, t, txmp, []byte{0})
	require.Equal(t, 1, txmp.Size())
	require.Equal(t, 1, txmp.gossipIndex.Len())
	{
		block, delivered := createProposalBlockAndDeliverTxs(txmp, height)
		require.Equal(t, 1, len(delivered))
		receiveTx(ctx, t, txmp, []byte{0}) // late receive from other `e.g. peer1` by gossiping
		receiveTx(ctx, t, txmp, []byte{0}) // late receive from other `e.g. peer2` by gossiping
		receiveTx(ctx, t, txmp, []byte{1})
		receiveTx(ctx, t, txmp, []byte{2})
		require.Equal(t, 3, txmp.Size())            // `map` can work as `set` by `tx`
		require.Equal(t, 5, txmp.gossipIndex.Len()) // `list` cannot work to avoid duplicate `tx`
		commitBlock(ctx, t, txmp, block, delivered)
		require.Equal(t, 2, txmp.Size())            // remove delivered a `tx`
		require.Equal(t, 4, txmp.gossipIndex.Len()) // remove delivered a `tx` and remains duplicate `txs`
		height++
	}
	printResultCounter()
}

func TestTxMempoolWithCacheSizeIsTwoSizeSmallerThanMempoolSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	height := int64(1)
	txmp := setupTxMempool(ctx, t, height, 3, 1) // size >= cacheSize + 2
	receiveTx(ctx, t, txmp, []byte{0})
	receiveTx(ctx, t, txmp, []byte{1}) // cache out: 0
	require.Equal(t, 2, txmp.Size())
	require.Equal(t, 2, txmp.gossipIndex.Len())
	{
		block, delivered := createProposalBlockAndDeliverTxs(txmp, height)
		require.Equal(t, 2, len(delivered))
		receiveTx(ctx, t, txmp, []byte{0}) // late receive from other `e.g. peer1` by gossiping
		receiveTx(ctx, t, txmp, []byte{2})
		require.Equal(t, 3, txmp.Size())            // `map` can work as `set` by `tx`
		require.Equal(t, 4, txmp.gossipIndex.Len()) // `list` cannot work to avoid duplicate `tx`
		commitBlock(ctx, t, txmp, block, delivered)
		require.Equal(t, 1, txmp.Size())            // remove delivered a `tx`
		require.Equal(t, 2, txmp.gossipIndex.Len()) // remove delivered a `tx` and remains duplicate `txs`
		height++
	}
	printResultCounter()
}

func TestTxMempoolWithCacheSizeDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	txmp := setupTxMempool(ctx, t, 1, -1, -1) // size=5000, cacheSize=10000
	go gossipRoutine(ctx, t, txmp)
	makeBlocksAndCommits(ctx, t, txmp)
	printResultCounter()
}

func createProposalBlockAndDeliverTxs(
	txmp *TxMempool, height int64) (*types.Block, []*abci.ResponseDeliverTx) {
	txs := txmp.ReapMaxBytesMaxGas(txmp.config.MaxTxsBytes, -1) // txmp.RLock/RUnlock
	block := types.MakeBlock(height, txs, nil, nil)
	deliverTxResponses := make([]*abci.ResponseDeliverTx, len(block.Txs))
	for i, tx := range block.Txs {
		deliverTxResponses[i] = &abci.ResponseDeliverTx{
			Code: abci.CodeTypeOK,
			Data: tx,
		}
	}
	return block, deliverTxResponses
}

func commitBlock(ctx context.Context, t testing.TB,
	txmp *TxMempool, block *types.Block, deliverTxResponses []*abci.ResponseDeliverTx) {
	txmp.Lock()
	defer txmp.Unlock()
	err := txmp.Update(ctx, block.Height, block.Txs, deliverTxResponses, nil, nil)
	require.NoError(t, err)
}

func receiveTx(ctx context.Context, t testing.TB, txmp *TxMempool, tx []byte) {
	atomic.AddInt64(&sent, 1)
	txInfo := TxInfo{}
	err := txmp.CheckTx(ctx, tx, abciCallback, txInfo) // txmp.RLock/RUnlock
	prepareCallback(err)
}

func prepareCallback(err error) {
	if err != nil {
		switch err {
		case types.ErrTxInCache:
			atomic.AddInt64(&failInCache, 1)
			break
		}
		switch err.(type) {
		case types.ErrTxTooLarge:
			atomic.AddInt64(&failTooLarge, 1)
			break
		case types.ErrMempoolIsFull:
			atomic.AddInt64(&failIsFull, 1)
			break
		case types.ErrPreCheck:
			atomic.AddInt64(&failPreCheck, 1)
			break
		}
	}
}

func abciCallback(res *abci.Response) {
	resCheckTx := res.GetCheckTx()
	if resCheckTx.Code != abci.CodeTypeOK && len(resCheckTx.Log) != 0 {
		atomic.AddInt64(&abciFail, 1)
	} else {
		atomic.AddInt64(&success, 1)
	}
}

var (
	sent         int64 = 0
	success      int64 = 0
	failInCache  int64 = 0
	failTooLarge int64 = 0
	failIsFull   int64 = 0
	failPreCheck int64 = 0
	abciFail     int64 = 0
)

func printResultCounter() {
	fmt.Printf("=====\n"+
		"          sent=%d\n"+
		"       success=%d\n"+
		" fail_in_cache=%d\n"+
		"fail_too_large=%d\n"+
		"  fail_is_full=%d\n"+
		"fail_pre_check=%d\n"+
		"     abci_fail=%d\n",
		sent, success, failInCache, failTooLarge, failIsFull, failPreCheck, abciFail)
}

func gossipRoutine(ctx context.Context, t testing.TB, txmp *TxMempool) {
	for i := 0; i < nodeNum; i++ {
		go receiveRoutine(ctx, t, txmp)
	}
}

func receiveRoutine(ctx context.Context, t testing.TB, txmp *TxMempool) {
	for {
		tx := []byte(strconv.Itoa(rand.Intn(txmp.config.CacheSize * 2)))
		// mempool.lock/unlock in CheckTxAsync
		receiveTx(ctx, t, txmp, tx)
		if sent%2000 == 0 {
			time.Sleep(time.Second) // for avoiding mempool full
		}
	}
}

func makeBlocksAndCommits(ctx context.Context, t testing.TB, txmp *TxMempool) {
	for i := 0; i < blockNum; i++ {
		block, deliverTxResponses := createProposalBlockAndDeliverTxs(txmp, int64(i+1))
		time.Sleep(randQuadraticCurveInterval(deliveredTimeMin, deliveredTimeMax, deliveredTimeRadix))
		commitBlock(ctx, t, txmp, block, deliverTxResponses)
		time.Sleep(randQuadraticCurveInterval(blockIntervalMin, blockIntervalMax, blockIntervalRadix))
	}
}

const (
	nodeNum            = 1
	blockNum           = 30
	blockIntervalMin   = 1.0 // second
	blockIntervalMax   = 1.0 // second
	blockIntervalRadix = 0.1
	deliveredTimeMin   = 2.0  // second
	deliveredTimeMax   = 10.0 // second
	deliveredTimeRadix = 0.1
)

func randQuadraticCurveInterval(min, max, radix float64) time.Duration {
	rand.Seed(time.Now().UnixNano())
	x := rand.Float64()*(max-min) + min
	y := (x * x) * radix
	return time.Duration(y*1000) * time.Millisecond
}
