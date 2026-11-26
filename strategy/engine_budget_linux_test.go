package strategy

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestLinuxEngineArgBudget(t *testing.T) {
	for _, tc := range []struct {
		stack uint64
		limit int
	}{
		{stack: 256 << 10, limit: linuxArgFloor},
		{stack: 8 << 20, limit: 2 << 20},
		{stack: ^uint64(0), limit: 6 << 20},
	} {
		got := linuxEngineArgBudget(tc.stack, 64<<10)
		if got.total != tc.limit-engineLauncherReserve || got.maxString != 2<<20 {
			t.Errorf("stack %d: budget = %+v", tc.stack, got)
		}
	}
	budget := linuxEngineArgBudget(8<<20, 4<<10)
	used := 0
	arg := strings.Repeat("x", budget.maxString-1)
	if err := budget.charge(&used, []string{arg}); err != nil || used != budget.maxString+8 {
		t.Fatalf("argument at string limit: %d, %v", used, err)
	}
	if err := budget.charge(&used, []string{arg + "x"}); !errors.Is(err, ErrEngineArgumentBudget) || !strings.Contains(err.Error(), "per-argument") {
		t.Fatalf("argument exceeding the kernel's string limit: %v", err)
	}
}

func TestEngineArgBudgetMatchesLinuxExec(t *testing.T) {
	const childStack = "SOLOVEY_TEST_ZAPRET_ARG_STACK"
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_STACK, &limit); err != nil {
		t.Fatal(err)
	}
	if value := os.Getenv(childStack); value != "" {
		stack, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		limit.Cur = stack
		if err := syscall.Setrlimit(syscall.RLIMIT_STACK, &limit); err != nil {
			t.Fatal(err)
		}
		budget, err := currentEngineArgBudget()
		if err != nil {
			t.Fatal(err)
		}
		arg := strings.Repeat("x", (64<<10)-1)
		args := []string{arg, arg, arg}
		used := 0
		budgetErr := budget.charge(&used, args)
		cmd := exec.Command("/bin/true", args...)
		cmd.Env = []string{"LANG=C", "LC_ALL=C"}
		execErr := cmd.Run()
		if stack == 256<<10 {
			if !errors.Is(budgetErr, ErrEngineArgumentBudget) || !errors.Is(execErr, syscall.E2BIG) {
				t.Fatalf("constrained stack: budget=%v, exec=%v", budgetErr, execErr)
			}
		} else if budgetErr != nil || execErr != nil {
			t.Fatalf("larger stack: budget=%v, exec=%v", budgetErr, execErr)
		}
		t.Logf("stack=%d available=%d charged=%d exec=%v", stack, budget.total, used, execErr)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, stack := range []uint64{256 << 10, 8 << 20} {
		value := strconv.FormatUint(stack, 10)
		t.Run(value, func(t *testing.T) {
			if limit.Max < stack {
				t.Skip("the inherited hard stack limit is smaller")
			}
			cmd := exec.Command(exe, "-test.run=^TestEngineArgBudgetMatchesLinuxExec$", "-test.v")
			cmd.Env = append(os.Environ(), childStack+"="+value)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("exec fixture: %v\n%s", err, output)
			}
			t.Logf("%s", output)
		})
	}
}
