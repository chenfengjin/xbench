package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/spf13/cobra"
	"github.com/xuperchain/xbench/cases"
	"github.com/xuperchain/xbench/lib"
)

// BenchCommand
type TransactionCommand struct {
	cli *Cli
	cmd *cobra.Command

	// 交易总量
	total int
	// 并发数
	concurrency int
	// 产出路径
	output string

	// 进程数
	process     int
	// 进程编号
	child       int

	host    string
	amount  string
}

func NewTransactionCommand(cli *Cli) *cobra.Command {
	t := new(TransactionCommand)
	t.cli = cli
	t.cmd = &cobra.Command{
		Use:   "tx",
		Short: "transaction",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.TODO()
			if t.process == 1 {
				return t.generate(ctx)
			}

			if err := lib.SplitTx(t.host, lib.Bank, t.amount, t.concurrency*t.process); err != nil {
				return err
			}

			wg := new(sync.WaitGroup)
			for i := 0; i < t.process; i++ {
				wg.Add(1)
				t.spawn(wg, i)
			}
			wg.Wait()
			return nil
		},
	}
	t.addFlags()
	return t.cmd
}

func (t *TransactionCommand) addFlags() {
	t.cmd.Flags().StringVarP(&t.host, "host", "", "", "get boot tx from host: ip:port")
	t.cmd.Flags().StringVarP(&t.amount, "amount", "", "100000000", "init amount")

	t.cmd.Flags().IntVarP(&t.total, "total", "t", 1000000, "total tx number")
	t.cmd.Flags().IntVarP(&t.concurrency, "concurrency", "c", 20, "goroutine concurrency number")
	t.cmd.Flags().StringVarP(&t.output, "output", "o", "../data/transaction", "generate tx output path")

	t.cmd.Flags().IntVarP(&t.process, "process", "", 1, "process number")
	t.cmd.Flags().IntVarP(&t.child, "child", "", 0, "child number")
}

func (t *TransactionCommand) spawn(wg *sync.WaitGroup, child int) error {
	cmd := exec.Command(os.Args[0],
		"tx",
		"--host", t.host,
		"--total", strconv.FormatInt(int64(t.total/t.process), 10),
		"--output", t.output,
		"--concurrency", strconv.Itoa(t.concurrency),
		"--process", "1",
		"--child", strconv.Itoa(child),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	go func() {
		defer wg.Done()
		err := cmd.Run()
		if err != nil {
			panic(err)
		}
	}()
	return nil
}

func (t *TransactionCommand) generate(ctx context.Context) error {
	config := &cases.Config{
		Host: t.host,
		Total: t.total,
		Concurrency: t.concurrency,
		Args: map[string]string{
			"amount": t.amount,
		},
	}
	generator, err := cases.NewTransaction(config)
	if err != nil {
		return fmt.Errorf("new transaction error: %v", err)
	}

	if err = generator.Init(); err != nil {
		return fmt.Errorf("init transaction error: %v", err)
	}

	encoders := make([]*json.Encoder, t.concurrency)
	for i := 0; i < t.concurrency; i++ {
		filename := fmt.Sprintf("transaction.dat.%04d", t.child*t.concurrency+i)
		file, err := os.Create(filepath.Join(t.output, filename))
		if err != nil {
			return fmt.Errorf("open output file error: %v", err)
		}
		encoders[i] = json.NewEncoder(file)
	}

	total := int(float32(t.total/t.concurrency)*1.1)
	Consumer(total, t.concurrency, generator, func(i int, tx proto.Message) error {
		if err := encoders[i].Encode(tx); err != nil {
			log.Fatalf("write tx error: %v", err)
			return err
		}
		return nil
	})

	log.Printf("child=%d, pid=%d", t.child, os.Getpid())
	return nil
}

type Consume func(i int, tx proto.Message) error
func Consumer(total, concurrency int, generator cases.Generator, consume Consume) {
	var inc int64
	wg := new(sync.WaitGroup)
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(i int) {
			defer wg.Done()
			var count int
			for {
				tx, err := generator.Generate(i)
				if err != nil {
					log.Fatalf("generate tx error: %v", err)
					return
				}

				if err = consume(i, tx); err != nil {
					return
				}

				count++
				if count % 1000 == 0 {
					newInc := atomic.AddInt64(&inc, 1000)
					if newInc % 100000 == 0 {
						log.Printf("total=%d", newInc)
					}
				}

				if count >= total {
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func init() {
	AddCommand(NewTransactionCommand)
	rand.Seed(time.Now().UnixNano())
}
