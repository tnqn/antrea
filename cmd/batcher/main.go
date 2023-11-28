package main

import (
	"fmt"
	"math/rand"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

type PR struct {
	ID   int
	Good bool
}

type Batch struct {
	PRs    []PR
	result bool
}

func newBatch(prs []PR) *Batch {
	return &Batch{
		PRs: prs,
	}
}

type Tester struct {
	cost           int
	duration       int
	maxConcurrency int
}

func (t *Tester) testBatch(batch *Batch) {
	t.cost += 1
	klog.V(2).InfoS("Testing", "PRs", batch.PRs)
	for _, pr := range batch.PRs {
		if !pr.Good {
			batch.result = false
			return
		}
	}
	batch.result = true
}

func (t *Tester) Test(batches ...*Batch) {
	t.duration += 1
	if len(batches) > t.maxConcurrency {
		t.maxConcurrency = len(batches)
	}
	for _, batch := range batches {
		t.testBatch(batch)
	}
}

func newPRsWithFailureNum(total int, failures int) []PR {
	prs := make([]PR, total)
	for i := 0; i < total; i++ {
		prs[i].ID = i
		prs[i].Good = i >= failures
	}
	rand.Shuffle(total, func(i, j int) {
		prs[i], prs[j] = prs[j], prs[i]
	})
	return prs
}

func newPRsWithFailureRate(total int, failureRate float64) []PR {
	prs := make([]PR, total)
	for i := 0; i < total; i++ {
		prs[i].ID = i
		prs[i].Good = rand.Float64() > failureRate
	}
	rand.Shuffle(total, func(i, j int) {
		prs[i], prs[j] = prs[j], prs[i]
	})
	return prs
}

func main() {
	strategies := []struct {
		name       string
		strategyFn strategyFunc
	}{
		{
			name:       "serial",
			strategyFn: serialStrategy,
		},
		{
			name:       "parallel",
			strategyFn: parallelStrategy,
		},
		{
			name:       "batch",
			strategyFn: batchStrategy,
		},
		{
			name:       "batch-bisect",
			strategyFn: batchBisectStrategy,
		},
	}
	var header string
	for _, strategy := range strategies {
		header += "," + strategy.name
	}
	fmt.Println(fmt.Sprintf("Cost%s,,Duration%s", header, header))
	for rate := 0; rate <= 50; rate++ {
		total := 10
		times := 10000
		failureRate := float64(rate) / 100
		klog.V(2).InfoS("Testing", "total", total, "failureRate", failureRate)
		costs := map[string]float64{}
		durations := map[string]float64{}

		for i := 0; i < times; i++ {
			prs := newPRsWithFailureRate(total, failureRate)
			for _, strategy := range strategies {
				cost, duration, _ := oneRound(strategy.strategyFn, prs)
				costs[strategy.name] += float64(cost)
				durations[strategy.name] += float64(duration)
			}
		}

		line := fmt.Sprintf("%.2f", failureRate)
		for _, strategy := range strategies {
			line += fmt.Sprintf(",%.4f", costs[strategy.name]/float64(times))
			klog.V(2).InfoS("Results", "strategy", strategy.name, "cost", costs[strategy.name]/float64(times))
		}
		line += fmt.Sprintf(",,%.2f", failureRate)
		for _, strategy := range strategies {
			line += fmt.Sprintf(",%.4f", durations[strategy.name]/float64(times))
			klog.V(2).InfoS("Results", "strategy", strategy.name, "duration", durations[strategy.name]/float64(times))
		}
		fmt.Println(line)
	}
}

type strategyFunc func(tester *Tester, prs []PR) []PR

func oneRound(strategyFn strategyFunc, prs []PR) (int, int, int) {
	tester := &Tester{}
	expectedFailedIDs := sets.New[int]()
	for _, pr := range prs {
		if !pr.Good {
			expectedFailedIDs.Insert(pr.ID)
		}
	}
	failed := strategyFn(tester, prs)
	gotFailedIDs := sets.New[int]()
	for _, pr := range failed {
		if !pr.Good {
			gotFailedIDs.Insert(pr.ID)
		}
	}
	if !gotFailedIDs.Equal(expectedFailedIDs) {
		panic("The strategy has a wrong implementation")
	}
	return tester.cost, tester.duration, tester.maxConcurrency

}

func batchBisectStrategy(tester *Tester, prs []PR) []PR {
	if len(prs) == 0 {
		return nil
	}
	failed := make([]PR, 0)
	var batches []*Batch
	single := newBatch(prs[:1])
	batches = append(batches, single)
	var all *Batch
	if len(prs) > 1 {
		all = newBatch(prs)
		batches = append(batches, all)
	}
	tester.Test(batches...)
	switch {
	case all == nil && single.result:
	case all == nil && !single.result:
		failed = append(failed, single.PRs[0])
	case all.result:
	case !all.result && single.result:
		prs = prs[1:]
		if len(prs) == 1 {
			failed = append(failed, prs[0])
		} else if len(prs) == 2 {
			first := newBatch(prs[:1])
			second := newBatch(prs[1:])
			tester.Test(first, second)
			if !first.result {
				failed = append(failed, first.PRs[0])
			}
			if !second.result {
				failed = append(failed, second.PRs[0])
			}
		} else {
			mid := (len(prs) + 1) / 2
			failed = append(failed, batchBisectStrategy(tester, prs[:mid])...)
			failed = append(failed, batchBisectStrategy(tester, prs[mid:])...)
		}
	case !single.result:
		failed = append(failed, single.PRs[0])
		failed = append(failed, batchBisectStrategy(tester, prs[1:])...)
	}
	return failed
}

func batchStrategy(tester *Tester, prs []PR) []PR {
	failed := make([]PR, 0)
	remains := prs[:]
	for {
		if len(remains) == 0 {
			break
		}
		var batches []*Batch
		single := newBatch(remains[:1])
		batches = append(batches, single)
		var all *Batch
		if len(remains) > 1 {
			all = newBatch(remains)
			batches = append(batches, all)
		}
		tester.Test(batches...)
		switch {
		case all == nil && single.result:
			remains = remains[1:]
		case all == nil && !single.result:
			remains = remains[1:]
			failed = append(failed, single.PRs[0])
		case all.result:
			remains = nil
		case !all.result && single.result:
			remains = remains[1:]
			if len(all.PRs) == 2 {
				remains = remains[1:]
				failed = append(failed, all.PRs[1])
			}
		case !single.result:
			remains = remains[1:]
			failed = append(failed, single.PRs[0])
		}
	}
	return failed
}

func parallelStrategy(tester *Tester, prs []PR) []PR {
	maxConcurrency := 5
	failed := make([]PR, 0)
	remains := prs[:]
	for {
		if len(remains) == 0 {
			break
		}
		var batches []*Batch
		for i := 1; i <= maxConcurrency; i++ {
			if len(remains) < i {
				break
			}
			batch := newBatch(remains[:i])
			batches = append(batches, batch)
		}
		tester.Test(batches...)
		for _, batch := range batches {
			if batch.result {
				remains = remains[1:]
			} else {
				failed = append(failed, batch.PRs[len(batch.PRs)-1])
				remains = remains[1:]
				break
			}
		}
	}
	return failed
}

func serialStrategy(tester *Tester, prs []PR) []PR {
	failed := make([]PR, 0)
	for _, pr := range prs {
		batch := &Batch{PRs: []PR{pr}}
		tester.Test(batch)
		if !batch.result {
			failed = append(failed, pr)
		}
	}
	return failed
}
