package kvpaxos

import "testing"
import "runtime"
import "strconv"
import "os"
import "time"
import "fmt"
import "math/rand"
import "strings"
//import "sync/atomic"


func check(t *testing.T, ck *Clerk, key string, value string) {
	v := ck.Get(key)
	if v != value {
		t.Fatalf("Get(%v) -> %v, expected %v", key, v, value)
	}
}

func port(tag string, host int) string {
	s := "/var/tmp/824-"
	s += strconv.Itoa(os.Getuid()) + "/"
	os.Mkdir(s, 0777)
	s += "kv-"
	s += strconv.Itoa(os.Getpid()) + "-"
	s += tag + "-"
	s += strconv.Itoa(host)
	return s
}

func cleanup(kva []*KVPaxos) {
	for i := 0; i < len(kva); i++ {
		if kva[i] != nil {
			kva[i].kill()
		}
	}
}

// predict effect of Append(k, val) if old value is prev.
func NextValue(prev string, val string) string {
	return prev + val
}




func randclerk(kvh []string) *Clerk {
	sa := make([]string, len(kvh))
	copy(sa, kvh)
	for i := range sa {
		j := rand.Intn(i + 1)
		sa[i], sa[j] = sa[j], sa[i]
	}
	return MakeClerk(sa)
}

// check that all known appends are present in a value,
// and are in order for each concurrent client.
func checkAppends(t *testing.T, v string, counts []int) {
	nclients := len(counts)
	for i := 0; i < nclients; i++ {
		lastoff := -1
		for j := 0; j < counts[i]; j++ {
			wanted := "x " + strconv.Itoa(i) + " " + strconv.Itoa(j) + " y"
			off := strings.Index(v, wanted)
			if off < 0 {
				t.Fatalf("missing element in Append result")
			}
			off1 := strings.LastIndex(v, wanted)
			if off1 != off {
				t.Fatalf("duplicate element in Append result")
			}
			if off <= lastoff {
				t.Fatalf("wrong order for element in Append result")
			}
			lastoff = off
		}
	}
}

func TestUnreliable(t *testing.T) {
	runtime.GOMAXPROCS(4)

	const nservers = 3
	var kva []*KVPaxos = make([]*KVPaxos, nservers)
	var kvh []string = make([]string, nservers)
	defer cleanup(kva)

	for i := 0; i < nservers; i++ {
		kvh[i] = port("un", i)
	}
	for i := 0; i < nservers; i++ {
		kva[i] = StartServer(kvh, i)
		kva[i].setunreliable(true)
	}

	ck := MakeClerk(kvh)
	var cka [nservers]*Clerk
	for i := 0; i < nservers; i++ {
		cka[i] = MakeClerk([]string{kvh[i]})
	}

	fmt.Printf("Test: Basic put/get, unreliable ...\n")

	ck.Put("a", "aa")
	check(t, ck, "a", "aa")

	cka[1].Put("a", "aaa")

	check(t, cka[2], "a", "aaa")
	check(t, cka[1], "a", "aaa")
	check(t, ck, "a", "aaa")

	fmt.Printf("  ... Passed\n")

	fmt.Printf("Test: Sequence of puts, unreliable ...\n")

	for iters := 0; iters < 6; iters++ {
		const ncli = 5
		var ca [ncli]chan bool
		for cli := 0; cli < ncli; cli++ {
			ca[cli] = make(chan bool)
			go func(me int) {
				ok := false
				defer func() { ca[me] <- ok }()
				myck := randclerk(kvh)
				key := strconv.Itoa(me)
				vv := myck.Get(key)
				myck.Append(key, "0")
				vv = NextValue(vv, "0")
				myck.Append(key, "1")
				vv = NextValue(vv, "1")
				myck.Append(key, "2")
				vv = NextValue(vv, "2")
				time.Sleep(100 * time.Millisecond)
				v := myck.Get(key) 
				if v != vv {
					t.Fatalf("wrong value, key %s, got %s, expected %s", key, v, vv)
				}
				v = myck.Get(key)
				if v != vv {
					t.Fatalf("wrong value, key %s, got %s, expected %s", key, v, vv)
				}
				ok = true
			}(cli)
		}
		for cli := 0; cli < ncli; cli++ {
			x := <-ca[cli]
			if x == false {
				t.Fatalf("failure")
			}
		}
	}

	fmt.Printf("  ... Passed\n")

	fmt.Printf("Test: Concurrent clients, unreliable ...\n")

	for iters := 0; iters < 20; iters++ {
		const ncli = 15
		var ca [ncli]chan bool
		for cli := 0; cli < ncli; cli++ {
			ca[cli] = make(chan bool)
			go func(me int) {
				defer func() { ca[me] <- true }()
				myck := randclerk(kvh)
				if (rand.Int() % 1000) < 500 {
					myck.Put("b", strconv.Itoa(rand.Int()))
				} else {
					myck.Get("b")
				}
			}(cli)
		}
		for cli := 0; cli < ncli; cli++ {
			<-ca[cli]
		}

		var va [nservers]string
		for i := 0; i < nservers; i++ {
			va[i] = cka[i].Get("b")
			if va[i] != va[0] {
				t.Fatalf("mismatch; 0 got %v, %v got %v", va[0], i, va[i])
			}
		}
	}

	fmt.Printf("  ... Passed\n")

	fmt.Printf("Test: Concurrent Append to same key, unreliable ...\n")

	ck.Put("k", "")

	ff := func(me int, ch chan int) {
		ret := -1
		defer func() { ch <- ret }()
		myck := randclerk(kvh)
		n := 0
		for n < 5 {
			myck.Append("k", "x "+strconv.Itoa(me)+" "+strconv.Itoa(n)+" y")
			n++
		}
		ret = n
	}

	ncli := 5
	cha := []chan int{}
	for i := 0; i < ncli; i++ {
		cha = append(cha, make(chan int))
		go ff(i, cha[i])
	}

	counts := []int{}
	for i := 0; i < ncli; i++ {
		n := <-cha[i]
		if n < 0 {
			t.Fatal("client failed")
		}
		counts = append(counts, n)
	}

	vx := ck.Get("k")
	checkAppends(t, vx, counts)

	{
		for i := 0; i < nservers; i++ {
			vi := cka[i].Get("k")
			if vi != vx {
				t.Fatalf("mismatch; 0 got %v, %v got %v", vx, i, vi)
			}
		}
	}

	fmt.Printf("  ... Passed\n")

	time.Sleep(1 * time.Second)
}


