package election

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/vntchain/go-vnt/common"
	"github.com/vntchain/go-vnt/core/state"
	inter "github.com/vntchain/go-vnt/core/vm/interface"
	"github.com/vntchain/go-vnt/vntdb"
)

var url = []byte("vnode://db881fae20f9339176beb89e8bc965d72e817c2994840033746a68ab5cc5260958192467aba695ec3747092ff3c56d09bab8061276f2c3c54ba992bdf8ceafd0@[::]:31303")

var InputCase = [][]byte{
	common.FromHex("c94ba774"),
	common.FromHex("68cc738800000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000a"),
	common.FromHex("95c96554"),
	common.FromHex("487a2abb"),
	common.FromHex("31f080ef"),
	common.FromHex("c9a63035"),
	common.FromHex("97107d6d000000000000000000000000a863d8efa01ece6fabfa7e8c85217a3c1af833a9"),
	common.FromHex("a694fc3a0000000000000000000000000000000000000000000000000000000000000064"),
	common.FromHex("73cf575a"),
	common.FromHex("efff125b00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000093656e6f64653a2f2f6266613730663133313962343063333139663961373930306266623836303563616531643631366636653131306264363431383533393763313664613635386562313039383061343566366663303335646135656364623435393330303835313130393663623034643031666636643434616335313230306239623930336661405b3a3a5d3a333033303300000000000000000000000000"),
	common.FromHex("f67ab93e"),
}
var candidates = []common.Address{
	common.BytesToAddress([]byte{1}),
	common.BytesToAddress([]byte{2}),
	common.BytesToAddress([]byte{3}),
	common.BytesToAddress([]byte{4}),
	common.BytesToAddress([]byte{5}),
	common.BytesToAddress([]byte{6}),
	common.BytesToAddress([]byte{7}),
	common.BytesToAddress([]byte{8}),
	common.BytesToAddress([]byte{9}),
}

type testContext struct {
	Origin  common.Address
	Time    *big.Int
	StateDB inter.StateDB
}

func (tc *testContext) GetOrigin() common.Address {
	return tc.Origin
}

func (tc *testContext) GetStateDb() inter.StateDB {
	return tc.StateDB
}

func (tc *testContext) GetTime() *big.Int {
	return tc.Time
}

func (tc *testContext) SetTime(t *big.Int) {
	tc.Time = t
}

func newcontext() inter.ChainContext {
	db := vntdb.NewMemDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db))
	c := testContext{
		Origin:  common.BytesToAddress([]byte{111}),
		Time:    big.NewInt(1531328510),
		StateDB: stateDB,
	}
	return &c
}

func getAllVoter(db inter.StateDB) []*Voter {
	var result []*Voter
	voters := make(map[common.Hash]common.Hash)
	addrs := make(map[common.Address]struct{})

	db.ForEachStorage(electionAddr, func(key common.Hash, value common.Hash) bool {
		if key[0] == VOTERPREFIX {
			voters[key] = value

			var addr common.Address
			copy(addr[:], key[PREFIXLENGTH:PREFIXLENGTH+common.AddressLength])
			addrs[addr] = struct{}{}
		}
		return true
	})

	getFn := func(key common.Hash) common.Hash {
		return voters[key]
	}

	for addr := range addrs {
		var voter Voter
		convertToStruct(VOTERPREFIX, addr, &voter, getFn)
		result = append(result, &voter)

	}
	return result
}

func checkValid(c electionContext) (bool, error) {
	// 保存原先context的时间
	currentTime := c.context.GetTime()

	proxyVote := make(map[common.Address]*big.Int)
	voteCount := make(map[common.Address]*big.Int)
	voters := getAllVoter(c.context.GetStateDb())
	// 循环一遍voter，做一遍初步的检查
	for _, voter := range voters {
		// 如果proxy不为空
		if !bytes.Equal(voter.Proxy.Bytes(), emptyAddress.Bytes()) {
			// 则isProxy为假，且voteCandidates为空
			if voter.IsProxy || len(voter.VoteCandidates) != 0 {
				return false, fmt.Errorf("voter owner: %x, proxy: %x, isProxy: %t, voteCandidates: %v\n", voter.Owner, voter.Proxy, voter.IsProxy, voter.VoteCandidates)
			}
			// 统计投给代理的票
			if count, ok := proxyVote[voter.Proxy]; ok {
				count.Add(count, voter.LastVoteCount)
				count.Add(count, voter.ProxyVoteCount)
			}
		}

		if voter.LastVoteCount != nil && voter.LastVoteCount.Sign() > 0 {
			if voter.TimeStamp == nil || voter.TimeStamp.Cmp(big.NewInt(0)) == 0 {
				return false, fmt.Errorf("lastVoteCount is not zero. timeStamp must not be nil")

			}
			// 比对lastVoteCount，是否是当时的抵押数兑换所得
			stake := c.getStake(voter.Owner)
			if stake.TimeStamp == nil || stake.TimeStamp.Cmp(big.NewInt(0)) == 0 {
				return false, fmt.Errorf("lastVoteCount is not zero. stake.timeStamp must not be nil")

			}
			// 抵押数大于0，且抵押时间小于投票时间，说明上次投票后抵押数没有变
			if stake.StakeCount.Sign() > 0 && stake.TimeStamp.Cmp(voter.TimeStamp) <= 0 {
				if ctx, ok := c.context.(*testContext); ok {
					ctx.SetTime(voter.TimeStamp)
				}
				// 计算抵押数可以兑换的票数，与上次投票所得是否一致
				calculateCount := c.calculateVoteCount(stake.StakeCount)
				if voter.LastVoteCount.Cmp(calculateCount) != 0 {
					if ctx, ok := c.context.(*testContext); ok {
						ctx.SetTime(currentTime)
					}
					return false, fmt.Errorf("time: %v, lastVoteCount : %d, stakeCount: %d,calculateCount : %d ",
						voter.TimeStamp, voter.LastVoteCount, stake.StakeCount, calculateCount)
				}
			}
			// 统计总投票
			for _, candi := range voter.VoteCandidates {
				if voteCount[candi] == nil {
					voteCount[candi] = big.NewInt(0)
				}
				voteCount[candi].Add(voteCount[candi], voter.LastVoteCount)
				voteCount[candi].Add(voteCount[candi], voter.ProxyVoteCount)
			}
		}

	}
	if ctx, ok := c.context.(*testContext); ok {
		ctx.SetTime(currentTime)
	}

	// 检查代理投票数
	for _, voter := range voters {
		if _, ok := proxyVote[voter.Owner]; !ok {
			continue
		}
		if voter.ProxyVoteCount.Cmp(proxyVote[voter.Owner]) != 0 {
			return false, fmt.Errorf("proxyVoteCount is wrong, proxyVoteCount in db: %d, expect proxyVote: %d\n", voter.ProxyVoteCount, proxyVote[voter.Owner])

		}
	}

	candidates := getAllCandidate(c.context.GetStateDb())
	for _, candidate := range candidates {
		if voteCount[candidate.Owner] == nil {
			voteCount[candidate.Owner] = big.NewInt(0)
		}
		if candidate.VoteCount == nil || candidate.VoteCount.Cmp(voteCount[candidate.Owner]) != 0 {
			return false, fmt.Errorf("voteCount is wrong. candidate address: %x,voteCount in db: %d, expect voteCount : %d", candidate.Owner, candidate.VoteCount, voteCount[candidate.Owner])
		}
	}

	return true, nil
}

func TestInput(t *testing.T) {
	var e Election
	context := newcontext()

	for _, input := range InputCase {
		_, err := e.Run(context, input)
		if err != nil && err == fmt.Errorf("call election contract err: method doesn't exist") {
			t.Error(err)
		}
	}
}

func TestCandidate_votes(t *testing.T) {
	var addr1 common.Address
	c1 := &Candidate{
		Owner:     addr1,
		VoteCount: big.NewInt(10),
		Active:    true,
	}

	if c1.votes().Cmp(big.NewInt(10)) != 0 {
		t.Errorf("votes() error. want = %v, got = %s", 10, c1.votes().String())
	}

	c1.Active = false
	if c1.votes().Cmp(big.NewInt(-10)) != 0 {
		t.Errorf("votes() error. want = %v, got = %s", -10, c1.votes().String())
	}
}

func TestCandidate_equal(t *testing.T) {
	addr1 := common.HexToAddress("0x122369f04f32269598789998de33e3d56e2c507a")
	addr2 := common.HexToAddress("0x42a875ac43f2b4e6d17f54d288071f5952bf8911")
	c1 := Candidate{
		Owner:     addr1,
		VoteCount: big.NewInt(10),
		Active:    true,
	}
	c2 := Candidate{
		Owner:     addr2,
		VoteCount: big.NewInt(20),
		Active:    false,
	}

	if c1.equal(&c2) {
		t.Errorf("two Candidate should not equal")
	}

	c1.Owner = addr2
	c1.Active = false
	c1.VoteCount = big.NewInt(20)

	if c1.equal(&c2) == false {
		t.Errorf("two Candidate should equal")
	}
}

func TestCandidateList_Less(t *testing.T) {
	addr1 := common.HexToAddress("0x122369f04f32269598789998de33e3d56e2c507a")
	addr2 := common.HexToAddress("0x42a875ac43f2b4e6d17f54d288071f5952bf8911")
	c1 := Candidate{
		Owner:     addr1,
		VoteCount: big.NewInt(10),
		Active:    true,
	}
	c2 := Candidate{
		Owner:     addr2,
		VoteCount: big.NewInt(20),
		Active:    false,
	}

	var cl CandidateList
	cl = append(cl, c1)
	cl = append(cl, c2)

	// actual is greater
	if cl.Less(0, 1) == false {
		t.Errorf("cl[0] should greater than cl[1]: %s, %s", cl[0].votes().String(), cl[1].votes().String())
	}
}

func TestCandidateList_Swap(t *testing.T) {
	addr1 := common.HexToAddress("0x122369f04f32269598789998de33e3d56e2c507a")
	addr2 := common.HexToAddress("0x42a875ac43f2b4e6d17f54d288071f5952bf8911")
	c1 := Candidate{
		Owner:     addr1,
		VoteCount: big.NewInt(10),
		Active:    true,
	}
	c2 := Candidate{
		Owner:     addr2,
		VoteCount: big.NewInt(20),
		Active:    false,
	}

	var cl CandidateList
	cl = append(cl, c1)
	cl = append(cl, c2)

	cl.Swap(0, 1)
	if cl[0].equal(&c2) == false {
		t.Errorf("cl[0] should equal c2")
	}
	if cl[1].equal(&c1) == false {
		t.Errorf("cl[1] should equal c1")
	}
}

// Test voteWitnesses
// 投的候选人过多返回错误
func TestVoteTooManyCandidates(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})

	var candidates []common.Address
	for i := 1; i <= voteLimit+1; i++ {
		candidate := common.BytesToAddress([]byte{byte(i)})
		candidates = append(candidates, candidate)
		c.registerWitness(candidate, url)
	}
	err := c.voteWitnesses(addr, candidates)
	if err.Error() != fmt.Sprintf("you voted too many candidates: the limit is %d, you voted %d", voteLimit, len(candidates)) {
		t.Error(err)
	}
}

// Test voteWitnesses
// 当前无抵押返回错误
func TestVoteWithoutStake(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)

	addr := common.BytesToAddress([]byte{111})

	// 不抵押投票，返回未抵押的错误
	err := c.voteWitnesses(addr, candidates)
	if err.Error() != "you must stake before vote" {
		t.Error(err)
	}
}

// Test voteWitnesses
// 距数据库中上次操作时间不足24小时，返回错误
func TestVoteWithoutEnoughTimeGap(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})

	// 数据库中塞一条voter数据
	voter := Voter{
		Owner:     addr,
		TimeStamp: big.NewInt(1531328500),
	}
	c.setVoter(voter)

	// 抵押
	c.context.GetStateDb().AddBalance(addr, big.NewInt(1e18))
	c.stake(addr, big.NewInt(1))
	// 投票
	err := c.voteWitnesses(addr, candidates)
	if err.Error() != fmt.Sprintf("it's less than 24h after your last vote or setProxy, lastTime: %v, now: %v", voter.TimeStamp, c.context.GetTime()) {
		t.Error(err)
	}
}

// Test voteWitnesses
// 原先没有投票也没有代理的情况
func TestVoteCandidatesFistTime(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)

	addr := common.BytesToAddress([]byte{111})
	c.context.GetStateDb().AddBalance(addr, big.NewInt(0).Mul(big.NewInt(10), big.NewInt(1e18)))
	c.stake(addr, big.NewInt(10))

	// 候选人注册
	for i := 0; i < len(candidates); i++ {
		c.registerWitness(candidates[i], url)
	}

	// 投票
	err := c.voteWitnesses(addr, candidates)
	if err != nil {
		t.Error(err)
	}

	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}
}

// Test cancelVote
// 数据库中无记录
func TestCancelVoteNoRecord(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)

	addr := common.BytesToAddress([]byte{111})
	err := c.cancelVote(addr)
	if err.Error() != fmt.Sprintf("the voter %x doesn't exist", addr) {
		t.Error(err)
	}
}

// Test cancelVote
// 数据库中无记录
func TestCancelVoteWithProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)

	addr := common.BytesToAddress([]byte{111})
	// 数据库中塞条记录
	voter := Voter{
		Owner:     addr,
		Proxy:     common.BytesToAddress([]byte{10}),
		TimeStamp: big.NewInt(1531328510),
	}
	c.setVoter(voter)

	err := c.cancelVote(addr)
	if err.Error() != fmt.Sprintf("must cancel proxy first, proxy: %x", voter.Proxy) {
		t.Error(err)
	}
}

// Test cancelVote
func TestCancelVote(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	addr1 := common.BytesToAddress([]byte{10})

	// 抵押1
	c.context.GetStateDb().AddBalance(addr, big.NewInt(0).Mul(big.NewInt(10), big.NewInt(1e18)))
	c.stake(addr, big.NewInt(10))

	// 抵押2
	c.context.GetStateDb().AddBalance(addr1, big.NewInt(0).Mul(big.NewInt(10), big.NewInt(1e18)))
	c.stake(addr, big.NewInt(100))

	// 设置候选人
	for i := 0; i < len(candidates); i++ {
		c.registerWitness(candidates[i], url)
	}

	// 投票1
	c.voteWitnesses(addr, candidates)
	voteCount := c.calculateVoteCount(big.NewInt(10))
	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}

	// 投票2
	c.voteWitnesses(addr1, candidates)
	voteCount1 := c.calculateVoteCount(big.NewInt(100))
	totalVoteCount := new(big.Int).Set(voteCount)
	totalVoteCount.Add(totalVoteCount, voteCount1)

	// 验证2
	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}

	// 取消投票
	c.cancelVote(addr)

	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}
}

// Test setProxy
// 设置代理人为自身返回错误
func TestProxySelf(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})

	err := c.setProxy(addr, addr)
	if err.Error() != "cannot proxy to self" {
		t.Error(err)
	}
}

// Test setProxy
// 设置代理人为自身
func TestProxySelfIsProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	proxy := common.BytesToAddress([]byte{10})
	// addr 成为代理
	c.startProxy(addr)

	err := c.setProxy(addr, proxy)
	if err.Error() != "account registered as a proxy is not allowed to use a proxy" {
		t.Error(err)
	}
}

// Test setProxy
// 当前无抵押返回错误
func TestProxyWithoutStake(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)

	addr := common.BytesToAddress([]byte{111})
	proxy := common.BytesToAddress([]byte{10})
	// 不抵押投票，返回未抵押的错误
	err := c.setProxy(addr, proxy)
	if err.Error() != "you must stake before vote" {
		t.Error(err)
	}
}

// Test setProxy
// 距数据库中上次操作时间不足24小时，返回错误
func TestProxyWithoutEnoughTimeGap(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	proxy := common.BytesToAddress([]byte{10})

	// 数据库中塞一条voter数据
	voter := Voter{
		Owner:     addr,
		TimeStamp: big.NewInt(1531328500),
	}
	c.setVoter(voter)

	// 抵押
	c.context.GetStateDb().AddBalance(addr, big.NewInt(1e18))
	c.stake(addr, big.NewInt(1))
	// 设置代理
	err := c.setProxy(addr, proxy)
	if err.Error() != fmt.Sprintf("it's less than 24h after your last vote or setProxy, lastTime: %v, now: %v", voter.TimeStamp, c.context.GetTime()) {
		t.Error(err)
	}
}

// Test setProxy
// 设置的代理人不是代理
func TestProxyIsNotProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	proxy := common.BytesToAddress([]byte{10})

	// 抵押
	c.context.GetStateDb().AddBalance(addr, big.NewInt(1e18))
	c.stake(addr, big.NewInt(1))
	// 设置代理
	err := c.setProxy(addr, proxy)
	if err.Error() != fmt.Sprintf("%x is not a proxy", proxy) {
		t.Error(err)
	}
}

func TestSetProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)

	err := setProxy(c)
	if err != nil {
		t.Error(err)
	}
}

// Test cancelProxy
// 数据库中无记录
func TestCancelProxyNoRecord(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})

	err := c.cancelProxy(addr)
	if err.Error() != "not set proxy" {
		t.Error(err)
	}
}

// Test cancelProxy
// 数据库中无记录
func TestCancelProxyNoProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	// 数据库中塞条记录
	voter := Voter{
		Owner:     addr,
		TimeStamp: big.NewInt(1531328500),
	}
	c.setVoter(voter)

	err := c.cancelProxy(addr)
	if err.Error() != "not set proxy" {
		t.Error(err)
	}
}

// Test cancelProxy
func TestCancelProxy(t *testing.T) {
	// 111->10
	context := newcontext()
	c := newElectionContext(context)

	addr := common.BytesToAddress([]byte{111})
	err := setProxy(c)
	if err != nil {
		t.Error(err)
	}
	voteCount := c.calculateVoteCount(big.NewInt(100))
	// 取消代理
	err = c.cancelProxy(addr)
	if err != nil {
		t.Error(err)
	}

	for i := 0; i < len(candidates); i++ {
		candi := c.getCandidate(candidates[i])
		if candi.VoteCount.Cmp(voteCount) != 0 {
			t.Errorf("The vote count %v is Wrong!", candi.VoteCount)
		}
	}

	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}
}

// Test startProxy
// 已经是代理了
func TestStartProxyWithIsProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	err := setProxy(c)
	if err != nil {
		t.Error(err)
	}

	err = c.startProxy(common.BytesToAddress([]byte{10}))
	if err.Error() != "startProxy proxy is already started" {
		t.Error(err)
	}
}

// Test startProxy
// 设置了代理的不可以成为代理
func TestStartProxyWithSetProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	err := setProxy(c)
	if err != nil {
		t.Error(err)
	}

	err = c.startProxy(common.BytesToAddress([]byte{111}))
	if err.Error() != "account that uses a proxy is not allowed to become a proxy" {
		t.Error(err)
	}
}

// Test stopProxy
// 数据库中无记录
func TestStopProxyNoRecord(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	err := c.stopProxy(addr)
	if err.Error() != "stopProxy proxy does not exist." {
		t.Error(err)
	}
}

// Test stopProxy
// 不是代理
func TestStopProxyNotProxy(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)

	voter := newVoter()
	voter.Owner = common.BytesToAddress([]byte{111})
	err := c.setVoter(voter)
	if err != nil {
		t.Error(err)
	}
	err = c.stopProxy(common.BytesToAddress([]byte{111}))
	if err.Error() != "stopProxy address is not proxy" {
		t.Error(err)
	}
}

func TestStartAndStopProxy(t *testing.T) {
	// addr: common.BytesToAddress([]byte{111}) 有10票
	// proxy: common.BytesToAddress([]byte{10}) 有100票
	// addr1: common.BytesToAddress([]byte{50}) 有20票
	// 一开始proxy是代理
	// addr和addr1都设置proxy为其代理
	// proxy停止代理，这个时候proxy身上有自身的票，addr和addr1的票
	// addr取消代理， 这个时候proxy身上有自身的票，addr1的票
	// proxy重新开始代理,接受新的人给它的代理

	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	addr1 := common.BytesToAddress([]byte{50})
	proxy := common.BytesToAddress([]byte{10})
	c.context.GetStateDb().AddBalance(addr1, big.NewInt(0).Mul(big.NewInt(20), big.NewInt(1e18)))
	c.stake(addr1, big.NewInt(20))

	// addr 设置 proxy为代理
	err := setProxy(c)
	if err != nil {
		t.Error(err)
	}

	// addr1 设置 proxy为代理
	err = c.setProxy(addr1, proxy)
	if err != nil {
		t.Error(err)
	}
	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}

	// 停止代理，原先代理的票都还有效
	c.stopProxy(proxy)
	if ctx, ok := c.context.(*testContext); ok {
		ctx.SetTime(big.NewInt(1531795552))
	}
	c.voteWitnesses(proxy, candidates)
	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}

	// 取消addr交给proxy的代理
	c.cancelProxy(addr)
	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}

	// 重新开始代理
	c.startProxy(proxy)
	c.unStake(addr1)
	c.context.GetStateDb().AddBalance(addr1, big.NewInt(0).Mul(big.NewInt(20), big.NewInt(1e18)))
	c.stake(addr1, big.NewInt(30))
	// 设置代理
	c.setProxy(common.BytesToAddress([]byte{100}), proxy)
	if _, err := checkValid(c); err != nil {
		t.Error(err)
	}
}

func TestStopAndSetProxy(t *testing.T) {
	// addr: common.BytesToAddress([]byte{111}) 有10票
	// proxy: common.BytesToAddress([]byte{10}) 有100票
	// addr1: common.BytesToAddress([]byte{50}) 有20票
	// 一开始proxy和addr1都是代理
	// addr开始设置proxy为其代理
	// proxy停止代理，并设置addr1为其代理，  这个时候addr1身上有自身的票，proxy的票和addr的票
	// addr1取消代理， 这个时候addr1身上有自身的票，proxy的票
	context := newcontext()
	c := newElectionContext(context)
	addr := common.BytesToAddress([]byte{111})
	addr1 := common.BytesToAddress([]byte{50})
	proxy := common.BytesToAddress([]byte{10})

	// addr 设置 proxy为代理
	err := setProxy(c)
	if err != nil {
		t.Error(err)
	}

	voteCount := c.calculateVoteCount(big.NewInt(10))
	voteCount1 := c.calculateVoteCount(big.NewInt(100))
	proxyVoteCount := big.NewInt(0)
	proxyVoteCount.Add(proxyVoteCount, voteCount)

	stake := Stake{
		Owner:      addr1,
		StakeCount: big.NewInt(20),
	}
	c.setStake(stake)

	// addr1 投票
	// addr1 开始代理
	c.startProxy(addr1)
	err = c.voteWitnesses(addr1, candidates)
	voteCount2 := c.calculateVoteCount(big.NewInt(20))
	totalVoteCount := big.NewInt(0)
	totalVoteCount.Add(voteCount, voteCount1)
	totalVoteCount.Add(totalVoteCount, voteCount2)

	if err != nil {
		t.Error(err)
	}
	for i := 0; i < len(candidates); i++ {
		candi := c.getCandidate(candidates[i])
		if candi.VoteCount.Cmp(totalVoteCount) != 0 {
			t.Errorf("The vote count %v is Wrong! Expected: %d", candi.VoteCount, totalVoteCount)
		}
	}

	// proxy 停止代理
	err = c.stopProxy(proxy)
	if err != nil {
		t.Error(err)
	}

	// proxy 设置代理
	if ctx, ok := c.context.(*testContext); ok {
		ctx.SetTime(big.NewInt(1531795552))
	}
	err = c.setProxy(proxy, addr1)
	totalVoteCount.Sub(totalVoteCount, voteCount1)
	voteCount1 = c.calculateVoteCount(big.NewInt(100))
	totalVoteCount.Add(totalVoteCount, voteCount1)

	if err != nil {
		t.Error(err)
	}

	for i := 0; i < len(candidates); i++ {
		candi := c.getCandidate(candidates[i])
		if candi.VoteCount.Cmp(totalVoteCount) != 0 {
			t.Errorf("The vote count %v is Wrong!", candi.VoteCount)
		}
	}

	// addr 取消 proxy代理
	c.cancelProxy(addr)
	totalVoteCount.Sub(totalVoteCount, voteCount)

	for i := 0; i < len(candidates); i++ {
		candi := c.getCandidate(candidates[i])
		if candi.VoteCount.Cmp(totalVoteCount) != 0 {
			t.Errorf("The vote count %v is Wrong!", candi.VoteCount)
		}
	}
}

func setProxy(c electionContext) error {
	addr := common.BytesToAddress([]byte{111})
	proxy := common.BytesToAddress([]byte{10})
	// 账户addr抵押
	c.context.GetStateDb().AddBalance(addr, big.NewInt(0).Mul(big.NewInt(10), big.NewInt(1e18)))
	c.stake(addr, big.NewInt(10))

	// 账户proxy抵押
	c.context.GetStateDb().AddBalance(proxy, big.NewInt(0).Mul(big.NewInt(100), big.NewInt(1e18)))
	c.stake(proxy, big.NewInt(100))

	// 账户proxy注册成为代理
	err := c.startProxy(proxy)
	if err != nil {
		return err
	}

	// 账户addr设置proxy
	err = c.setProxy(addr, proxy)
	if err != nil {
		return err
	}

	// 设置候选人
	for i := 0; i < len(candidates); i++ {
		c.registerWitness(candidates[i], url)
	}

	// 代理人投票
	err = c.voteWitnesses(proxy, candidates)
	if err != nil {
		return err
	}

	if _, err := checkValid(c); err != nil {
		return err
	}
	return nil
}

func TestRegisterWitness(t *testing.T) {
	context := newcontext()
	ec := newElectionContext(context)

	addr1 := common.HexToAddress("41b0db166cfdf1c4ba3ce657171482a9aa55cc93")
	addr2 := common.HexToAddress("08b467a881ec34b668254aa956e0c46f9c3b2b83")
	addr3 := common.HexToAddress("0c0292587ccdc76b8f449002a017bc9479ff0a88")

	t.Logf("addr1: %v", addr1.Hex())
	t.Logf("addr2: %v", addr2.Hex())
	t.Logf("addr3: %v", addr3.Hex())

	err := ec.registerWitness(addr1, url)
	if err != nil {
		t.Errorf("TestRegisterWitness registerWitness err:%v", err)
	}

	candis := getAllCandidate(context.GetStateDb())
	for _, candi := range candis {
		t.Logf("111 addr: %v, voteCount: %v, active: %v", candi.Owner.Hex(), candi.VoteCount, candi.Active)
	}

	err = ec.registerWitness(addr1, url)
	if err.Error() != "registerWitness witness already exists" {
		t.Errorf("TestRegisterWitness registerWitness err:%v", err)
	}

	candis = getAllCandidate(context.GetStateDb())
	for _, candi := range candis {
		t.Logf("222 addr: %v, voteCount: %v, active: %v", candi.Owner.Hex(), candi.VoteCount, candi.Active)
	}

	err = ec.registerWitness(addr2, url)
	if err != nil {
		t.Errorf("TestRegisterWitness registerWitness err:%v", err)
	}

	err = ec.registerWitness(addr3, url)
	if err != nil {
		t.Errorf("TestRegisterWitness registerWitness err:%v", err)
	}

	candis = getAllCandidate(context.GetStateDb())
	for _, candi := range candis {
		t.Logf("333 addr: %v, voteCount: %v, active: %v", candi.Owner.Hex(), candi.VoteCount, candi.Active)
	}

	err = ec.unregisterWitness(addr1)
	if err != nil {
		t.Errorf("TestRegisterWitness unregisterWitness err:%v", err)
	}

	candis = getAllCandidate(context.GetStateDb())
	for _, candi := range candis {
		t.Logf("444 addr: %v, voteCount: %v, active: %v", candi.Owner.Hex(), candi.VoteCount, candi.Active)
	}

	err = ec.unregisterWitness(addr3)
	if err != nil {
		t.Errorf("TestRegisterWitness unregisterWitness err:%v", err)
	}

	candis = getAllCandidate(context.GetStateDb())
	for _, candi := range candis {
		t.Logf("555 addr: %v, voteCount: %v, active: %v", candi.Owner.Hex(), candi.VoteCount, candi.Active)
	}

	err = ec.unregisterWitness(addr2)
	if err != nil {
		t.Errorf("TestRegisterWitness unregisterWitness err:%v", err)
	}

	candis = getAllCandidate(context.GetStateDb())
	for _, candi := range candis {
		t.Logf("666 addr: %v, voteCount: %v, active: %v", candi.Owner.Hex(), candi.VoteCount, candi.Active)
	}
}

func TestRegisterProxy(t *testing.T) {
	context := newcontext()
	ec := newElectionContext(context)

	addr1 := common.HexToAddress("41b0db166cfdf1c4ba3ce657171482a9aa55cc93")
	addr2 := common.HexToAddress("08b467a881ec34b668254aa956e0c46f9c3b2b83")
	addr3 := common.HexToAddress("0c0292587ccdc76b8f449002a017bc9479ff0a88")

	t.Logf("addr1: %v", addr1.Hex())
	t.Logf("addr2: %v", addr2.Hex())
	t.Logf("addr3: %v", addr3.Hex())

	err := ec.startProxy(addr1)
	if err != nil {
		t.Errorf("TestRegisterProxy startProxy err: %v", err)
	}

	proxys := getAllProxy(context.GetStateDb())
	for _, proxy := range proxys {
		t.Logf("111 proxy %v", proxy.Owner.Hex())
	}

	err = ec.stopProxy(addr1)
	if err != nil {
		t.Errorf("TestRegisterProxy startProxy err: %v", err)
	}

	proxys = getAllProxy(context.GetStateDb())
	for _, proxy := range proxys {
		t.Logf("222 proxy %v", proxy.Owner.Hex())
	}

	err = ec.startProxy(addr2)
	if err != nil {
		t.Errorf("TestRegisterProxy startProxy err: %v", err)
	}

	err = ec.startProxy(addr3)
	if err != nil {
		t.Errorf("TestRegisterProxy startProxy err: %v", err)
	}

	proxys = getAllProxy(context.GetStateDb())
	for _, proxy := range proxys {
		t.Logf("333 proxy %v", proxy.Owner.Hex())
	}
}

func TestStake(t *testing.T) {
	context := newcontext()
	ec := newElectionContext(context)

	addr1 := common.HexToAddress("41b0db166cfdf1c4ba3ce657171482a9aa55cc93")

	context.GetStateDb().AddBalance(addr1, big.NewInt(0).Mul(big.NewInt(10000000000), big.NewInt(10000000000)))

	err := ec.stake(addr1, big.NewInt(20))
	if err != nil {
		t.Errorf("TestStake stake err:%v ", err)
	}

	t.Logf("111 addr1 balance: %v", context.GetStateDb().GetBalance(addr1))

	stake := ec.getStake(addr1)
	t.Logf("111 addr1 stake: %v", stake.StakeCount)

	err = ec.unStake(addr1)
	if err.Error() != "cannot unstake in 24 hours" {
		t.Errorf("TestStake unStake err:%v ", err)
	}

	t.Logf("222 addr1 balance after unStake: %v", context.GetStateDb().GetBalance(addr1))

	stake = ec.getStake(addr1)
	t.Logf("222 addr1 stake after unStake: %v", stake.StakeCount)

	if ctx, ok := context.(*testContext); ok {
		ctx.SetTime(big.NewInt(0).Add(context.GetTime(), big.NewInt(3600*24+1)))
	}
	err = ec.unStake(addr1)
	if err != nil {
		t.Errorf("TestStake unStake err:%v ", err)
	}

	t.Logf("333 addr1 balance after unStake: %v", context.GetStateDb().GetBalance(addr1))

	stake = ec.getStake(addr1)
	t.Logf("333 addr1 stake after unStake: %v", stake.StakeCount)

	err = ec.stake(addr1, big.NewInt(-20))
	if err.Error() != "stake stakeCount less than 0" {
		t.Errorf("TestStake stake err:%v ", err)
	}

	t.Logf("444 addr1 balance: %v", context.GetStateDb().GetBalance(addr1))

	stake = ec.getStake(addr1)
	t.Logf("444 addr1 stake: %v", stake.StakeCount)
}

func TestExtractBounty(t *testing.T) {
	context := newcontext()
	ec := newElectionContext(context)
	ec.setCandidate(candidate)
	if err := ec.extractOwnBounty(candidate.Owner); err != nil {
		t.Error(err)
	}
	candidate1 := ec.getCandidate(candidate.Owner)
	if candidate1.TotalBounty.Cmp(candidate1.ExtractedBounty) != 0 {
		t.Errorf("extracted bounty %v not equal to totalBouty %v", candidate1.ExtractedBounty, candidate1.TotalBounty)
	}
}

func TestGrantBounty(t *testing.T) {
	context := newcontext()

	if err := setRestBounty(context.GetStateDb(), bounty); err != nil {
		t.Error(err)
	}
	// enough to pay
	if rest, err := GrantBounty(context.GetStateDb(), big.NewInt(1e17)); err != nil {
		t.Error(err)
	} else if rest.Cmp(big.NewInt(9e17)) != 0 {
		t.Error("the rest of bounty error")
	}

	// not enough to pay
	if rest, err := GrantBounty(context.GetStateDb(), big.NewInt(1e18)); err == nil {
		t.Error("the rest of bounty should be not enough to pay")
	} else if rest.Cmp(big.NewInt(9e17)) != 0 {
		t.Error("the rest of bounty error")
	}

	// just to pay
	if rest, err := GrantBounty(context.GetStateDb(), big.NewInt(9e17)); err != nil {
		t.Log(err)
	} else if rest.Cmp(big.NewInt(0)) != 0 {
		t.Error("the rest of bounty error")
	}
}

func TestCalculateVote(t *testing.T) {
	context := newcontext()
	ec := newElectionContext(context)
	if ctx, ok := context.(*testContext); ok {
		ctx.SetTime(big.NewInt(1514736000))
	}

	stakeCount := big.NewInt(10000000)
	voteCount := ec.calculateVoteCount(stakeCount)
	if voteCount.Cmp(big.NewInt(10000000)) != 0 {
		t.Errorf("VoteCount %d value wrong ! It should be 10.", voteCount)
	}

	if ctx, ok := context.(*testContext); ok {
		ctx.SetTime(big.NewInt(1532080414))
	}
	voteCount = ec.calculateVoteCount(stakeCount)
	if voteCount.Cmp(big.NewInt(14524228)) != 0 {
		t.Errorf("VoteCount %d value wrong ! It should be 10.", voteCount)
	}

	if ctx, ok := context.(*testContext); ok {
		ctx.SetTime(big.NewInt(1532225103))
	}
	voteCount = ec.calculateVoteCount(stakeCount)
	if voteCount.Cmp(big.NewInt(14524228)) != 0 {
		t.Errorf("VoteCount %d value wrong ! It should be 10.", voteCount)
	}
}

var operates []string            // 有哪些操作
var alreadySet map[byte]struct{} // alreadySet用来标记节点是否已经扩展过

func dfsState(c electionContext, address common.Address, checkFn func(byte, string, byte) error) error {
	// 进入时保存当前数据库状态，退出时恢复
	snap := c.context.GetStateDb().Snapshot()
	defer c.context.GetStateDb().RevertToSnapshot(snap)
	currentState := checkState(c, address)
	for _, op := range operates {
		snap1 := c.context.GetStateDb().Snapshot()
		err := operateOnState(c, op, address)
		if err == nil {
			if _, err = checkValid(c); err != nil {
				return err
			}
			nextState := checkState(c, address)
			// 比较到达的状态是否与预期状态一致
			if err = checkFn(currentState, op, nextState); err != nil {
				return err
			}
			// 如果是新的状态加入到队列中
			if _, ok := alreadySet[nextState]; !ok {
				alreadySet[nextState] = struct{}{}
				err = dfsState(c, address, checkFn)
				if err != nil {
					return err
				}
			}
		}
		c.context.GetStateDb().RevertToSnapshot(snap1)
	}
	return nil
}

func TestVoteAndProxyState1(t *testing.T) {
	// 定义两种角色addr做为普通用户，操作只有投票，取消投票，设置代理，取消设置代理四种
	// proxy做为代理用户，操作有投票，取消投票，设置代理，取消设置代理四种，还有startProxy和stopProxy
	// addrStateMap存预先设置好addr相关的状态转化图，proxyStateMap存proxy的状态转化图
	addr := common.BytesToAddress([]byte{111})
	operates = []string{
		"voteWitnesses",
		"cancelVote",
		"setProxy",
		"cancelProxy",
	}

	// addr的状态转化图
	addrStateMap := make(map[byte]map[string]byte)

	// 0无投票无代理，4无投票有代理，8有投票无代理
	addrStateMap[0] = make(map[string]byte)
	addrStateMap[8] = make(map[string]byte)
	addrStateMap[2] = make(map[string]byte)

	addrStateMap[0]["voteWitnesses"] = 8
	addrStateMap[0]["setProxy"] = 2
	addrStateMap[0]["cancelVote"] = 0
	addrStateMap[8]["cancelVote"] = 0
	addrStateMap[8]["setProxy"] = 2
	addrStateMap[8]["voteWitnesses"] = 8
	addrStateMap[2]["voteWitnesses"] = 8
	addrStateMap[2]["cancelProxy"] = 0
	addrStateMap[2]["setProxy"] = 2

	// 抵押一类的初始操作
	context := newcontext()
	c := newElectionContext(context)
	initForStateTest(c)

	alreadySet = make(map[byte]struct{})
	alreadySet[0] = struct{}{}
	checkFn := func(current byte, op string, next byte) error {
		if addrStateMap[current][op] != next {
			return fmt.Errorf("state error ,current state %d, op %s, nextState %d, expected %d", current, op, next, addrStateMap[current][op])
		}
		return nil
	}

	if err := dfsState(c, addr, checkFn); err != nil {
		t.Error(err)
	}
}

func TestVoteAndProxyState2(t *testing.T) {
	// 定义两种角色addr做为普通用户，操作只有投票，取消投票，设置代理，取消设置代理四种
	// proxy做为代理用户，操作有投票，取消投票，设置代理，取消设置代理四种，还有startProxy和stopProxy
	// addrStateMap存预先设置好addr相关的状态转化图，proxyStateMap存proxy的状态转化图
	proxy := common.BytesToAddress([]byte{10})
	proxyStateMap := make(map[byte]map[string]byte)

	// 代理的一些状态转变不是自身发起的
	proxyOperates := []string{
		"voteWitnesses",
		"cancelVote",
		"setProxy",
		"cancelProxy",
		"startProxy",
		"stopProxy",
		"addrSetProxy",
		"addrCancelProxy",
	}
	operates = proxyOperates
	// 4个标志位，一共16种，其中有6中状态是不合法的
	for i := 0; i < 14; i++ {
		if i == 6 || i == 7 || i == 10 {
			continue
		}
		proxyStateMap[byte(i)] = make(map[string]byte)
	}
	proxyStateMap[0]["voteWitnesses"] = 8
	proxyStateMap[0]["cancelVote"] = 0
	proxyStateMap[0]["startProxy"] = 4
	proxyStateMap[0]["setProxy"] = 2
	proxyStateMap[0]["stopProxy"] = 0

	proxyStateMap[1]["voteWitnesses"] = 9
	proxyStateMap[1]["cancelVote"] = 1
	proxyStateMap[1]["startProxy"] = 5
	proxyStateMap[1]["setProxy"] = 3
	proxyStateMap[1]["stopProxy"] = 1
	proxyStateMap[1]["addrCancelProxy"] = 0

	proxyStateMap[2]["voteWitnesses"] = 8
	proxyStateMap[2]["setProxy"] = 2
	proxyStateMap[2]["stopProxy"] = 2
	proxyStateMap[2]["cancelProxy"] = 0

	proxyStateMap[3]["voteWitnesses"] = 9
	proxyStateMap[3]["setProxy"] = 3
	proxyStateMap[3]["stopProxy"] = 3
	proxyStateMap[3]["cancelProxy"] = 1
	proxyStateMap[3]["addrCancelProxy"] = 2

	proxyStateMap[4]["voteWitnesses"] = 12
	proxyStateMap[4]["cancelVote"] = 4
	proxyStateMap[4]["startProxy"] = 4
	proxyStateMap[4]["stopProxy"] = 0
	proxyStateMap[4]["addrSetProxy"] = 5

	proxyStateMap[5]["voteWitnesses"] = 13
	proxyStateMap[5]["cancelVote"] = 5
	proxyStateMap[5]["startProxy"] = 5
	proxyStateMap[5]["stopProxy"] = 1
	proxyStateMap[5]["addrSetProxy"] = 5
	proxyStateMap[5]["addrCancelProxy"] = 4

	proxyStateMap[8]["cancelVote"] = 0
	proxyStateMap[8]["voteWitnesses"] = 8
	proxyStateMap[8]["setProxy"] = 2
	proxyStateMap[8]["startProxy"] = 12
	proxyStateMap[8]["stopProxy"] = 8

	proxyStateMap[9]["cancelVote"] = 1
	proxyStateMap[9]["setProxy"] = 3
	proxyStateMap[9]["voteWitnesses"] = 9
	proxyStateMap[9]["stopProxy"] = 9
	proxyStateMap[9]["startProxy"] = 13
	proxyStateMap[9]["addrCancelProxy"] = 8

	proxyStateMap[12]["cancelVote"] = 4
	proxyStateMap[12]["voteWitnesses"] = 12
	proxyStateMap[12]["startProxy"] = 12
	proxyStateMap[12]["stopProxy"] = 8
	proxyStateMap[12]["addrSetProxy"] = 13

	proxyStateMap[13]["cancelVote"] = 5
	proxyStateMap[13]["voteWitnesses"] = 13
	proxyStateMap[13]["startProxy"] = 13
	proxyStateMap[13]["stopProxy"] = 9
	proxyStateMap[13]["addrSetProxy"] = 13
	proxyStateMap[13]["addrCancelProxy"] = 12

	checkFn := func(current byte, op string, next byte) error {
		if proxyStateMap[current][op] != next {
			return fmt.Errorf("state error ,current state %d, op %s, nextState %d, expected %d", current, op, next, proxyStateMap[current][op])
		}
		return nil
	}

	// 抵押一类的初始操作
	context := newcontext()
	c := newElectionContext(context)
	initForStateTest(c)

	alreadySet = make(map[byte]struct{})

	alreadySet[4] = struct{}{}
	if err := dfsState(c, proxy, checkFn); err != nil {
		t.Error(err)
	}
}

func initForStateTest(c electionContext) {
	addr := common.BytesToAddress([]byte{111})
	proxy := common.BytesToAddress([]byte{10})
	proxy1 := common.BytesToAddress([]byte{50})
	if ctx, ok := c.context.(*testContext); ok {
		ctx.SetTime(new(big.Int).Set(eraTimeStamp))
	}

	c.context.GetStateDb().AddBalance(addr, big.NewInt(0).Mul(big.NewInt(10), big.NewInt(1e18)))
	c.stake(addr, big.NewInt(10))
	c.context.GetStateDb().AddBalance(proxy, big.NewInt(0).Mul(big.NewInt(100), big.NewInt(1e18)))
	c.stake(proxy, big.NewInt(100))
	c.context.GetStateDb().AddBalance(proxy1, big.NewInt(0).Mul(big.NewInt(1000), big.NewInt(1e18)))
	c.stake(proxy1, big.NewInt(1000))
	c.startProxy(proxy)
	c.startProxy(proxy1)
	c.voteWitnesses(proxy1, candidates)

	// 下面只是为了把addr塞进数据库
	c.startProxy(addr)
	c.stopProxy(addr)

	for _, candi := range candidates {
		c.registerWitness(candi, url)
	}
}

func operateOnState(c electionContext, op string, address common.Address) error {
	addr := address
	proxy := common.BytesToAddress([]byte{10})
	switch op {
	case "setProxy":
		if bytes.Equal(address.Bytes(), common.BytesToAddress([]byte{10}).Bytes()) {
			proxy = common.BytesToAddress([]byte{50})
		}
	case "addrSetProxy":
		fallthrough
	case "addrCancelProxy":
		addr = common.BytesToAddress([]byte{111})
	}
	return operate(c, op, addr, proxy, candidates)
}

func checkState(c electionContext, address common.Address) byte {
	voter := c.getVoter(address)
	if !bytes.Equal(voter.Owner.Bytes(), address.Bytes()) {
		return 0
	}
	var result byte
	// 有投票,判断voteCandidates,或者无代理却有投票数(上次投了个空票)
	if len(voter.VoteCandidates) > 0 {
		result |= 1 << 3
	} else if bytes.Equal(voter.Proxy.Bytes(), emptyAddress.Bytes()) && voter.LastVoteCount.Sign() > 0 {
		result |= 1 << 3
	}
	// 是代理
	if voter.IsProxy {
		result |= 1 << 2
	}
	// 有代理
	if !bytes.Equal(voter.Proxy.Bytes(), emptyAddress.Bytes()) {
		result |= 1 << 1
	}
	// 有代理投票
	if voter.ProxyVoteCount.Sign() > 0 {
		result |= 1
	}
	return result
}

// For All test
// 定义6个账户(复杂度为指数级，定义账户过多运行时间过长)，账户地址50-55。其中
// 2个账户为一般用户，它们可以投票、取消投票、设置代理、取消代理。
// 2个账户为代理账户(方便出现类似多级代理的情况)，它们比一般账户多了开始代理和停止代理两个操作。
// 1个账户为候选账户，它们只有注册成为见证人和注销见证人两个操作。
// 1个账户为全能账户，它们可以进行所有的操作。
var alreadySetForAllStateTest map[[6]byte]struct{}
var operatesForAllStateTest = []string{
	"voteWitnesses",
	"cancelVote",
	"setProxy",
	"cancelProxy",
	"startProxy",
	"stopProxy",
	"registerWitness",
	"unregisterWitness",
}

// 检查并返回所有20个账户的当前状态
// 状态有6个指标
// 是否是候选人
// 是否有抵押
// 是否有投票
// 是否是代理
// 是否有代理
// 是否有代理投票
func checkAllState(c electionContext) [6]byte {
	var result [6]byte
	var resultByte byte
	for i := 0; i < 6; i++ {
		addr := common.BytesToAddress([]byte{byte(i + 50)})
		resultByte = 0

		//// 是否是候选人
		if i > 3 {
			candidate := c.getCandidate(addr)
			if bytes.Equal(candidate.Owner.Bytes(), addr.Bytes()) {
				if candidate.Active {
					resultByte |= 1 << 4
				}
			}
		}
		resultByte |= checkState(c, addr)
		result[i] = resultByte
	}
	return result
}

func operate(c electionContext, op string, address common.Address, proxy common.Address, candidates []common.Address) error {
	var err error
	switch op {
	case "voteWitnesses":
		if ctx, ok := c.context.(*testContext); ok {
			t := c.context.GetTime()
			ctx.SetTime(t.Add(t, big.NewInt(24*3600+1)))
		}
		err = c.voteWitnesses(address, candidates)
	case "cancelVote":
		err = c.cancelVote(address)
	case "setProxy":
		if ctx, ok := c.context.(*testContext); ok {
			t := c.context.GetTime()
			ctx.SetTime(t.Add(t, big.NewInt(24*3600+1)))
		}
		err = c.setProxy(address, proxy)
	case "cancelProxy":
		err = c.cancelProxy(address)
	case "startProxy":
		err = c.startProxy(address)
	case "stopProxy":
		err = c.stopProxy(address)
	case "registerWitness":
		err = c.registerWitness(address, url)
	case "unregisterWitness":
		err = c.unregisterWitness(address)
	default:
		err = fmt.Errorf("method not found")
	}
	return err
}

func dfsAllState(c electionContext) error {
	// 进入时保存当前数据库状态，退出时恢复
	snap := c.context.GetStateDb().Snapshot()
	defer c.context.GetStateDb().RevertToSnapshot(snap)
	// 迭代产生address
	for i := 50; i < 56; i++ {
		address := common.BytesToAddress([]byte{byte(i)})
		// 迭代产生proxy
		for j := 52; j < 56; j++ {
			if j == 54 {
				continue
			}
			proxy := common.BytesToAddress([]byte{byte(j)})
			// 迭代产生candidates
			candis := []common.Address{
				common.BytesToAddress([]byte{byte(54)}),
				common.BytesToAddress([]byte{byte(55)}),
			}

			// 迭代各种操作
			for idx, op := range operatesForAllStateTest {
				// 一般账号，没有后面4个操作
				if (i == 50 || i == 51) && idx > 3 {
					continue
				} else if (i == 52 || i == 53) && idx > 5 { // 代理账号，没有后面2个操作
					continue
				} else if (i == 54) && (idx < 6) { // 候选人账号，只有两个操作
					continue
				}
				snap1 := c.context.GetStateDb().Snapshot()
				time1 := new(big.Int).Set(c.context.GetTime())
				err := operate(c, op, address, proxy, candis)

				if err == nil {
					if _, err = checkValid(c); err != nil {
						fmt.Println(err)
						return err
					}
					nextState := checkAllState(c)
					// 如果是新的状态加入到队列中
					if _, ok := alreadySetForAllStateTest[nextState]; !ok {
						alreadySetForAllStateTest[nextState] = struct{}{}
						err = dfsAllState(c)
						if err != nil {
							return err
						}
					}
				}
				c.context.GetStateDb().RevertToSnapshot(snap1)
				if ctx, ok := c.context.(*testContext); ok {
					ctx.SetTime(time1)
				}
			}

		}
	}
	return nil
}

func TestAllState(t *testing.T) {
	context := newcontext()
	c := newElectionContext(context)
	for i := 50; i < 56; i++ {
		addr := common.BytesToAddress([]byte{byte(i)})
		c.context.GetStateDb().AddBalance(addr, big.NewInt(1).Mul(big.NewInt(int64(i)), big.NewInt(1e18)))
		c.stake(addr, big.NewInt(int64(i)))
		if i >= 54 {
			c.registerWitness(addr, url)
		}
	}
	alreadySetForAllStateTest = make(map[[6]byte]struct{})
	var start [6]byte
	start[4] = 16
	start[5] = 16
	alreadySetForAllStateTest[start] = struct{}{}
	err := dfsAllState(c)
	if err != nil {
		t.Error(err)
	}

	test := make(map[int]map[byte]struct{})
	for i := 50; i < 56; i++ {
		test[i] = make(map[byte]struct{})
	}

	for k := range alreadySetForAllStateTest {
		for i := 0; i < 6; i++ {
			test[50+i][k[i]] = struct{}{}
		}
	}
	if len(test[50]) != 3 || len(test[51]) != 3 {
		t.Errorf("the number of state for general account is : (%d, %d), expected: %d", len(test[50]), len(test[51]), 3)
	}
	if len(test[52]) != 10 || len(test[53]) != 10 {
		t.Errorf("the number of state for proxy account is : (%d, %d), expected: %d", len(test[52]), len(test[53]), 10)
	}
	if len(test[54]) != 2 {
		t.Errorf("the number of state for candidate account is : %d, expected: %d", len(test[54]), 2)
	}
	if len(test[55]) != 20 {
		t.Errorf("the number of state for super account is : %d, expected: %d", len(test[55]), 20)
	}
}
