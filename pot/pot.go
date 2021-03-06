package pot

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	noPosToAct = -1
)

type Action int

const (
	Fold Action = iota
	Check
	Call
	Bet
	Raise
	AllIn
)

var actionStrs = []string{"Fold", "Check", "Call", "Bet", "Raise", "AllIn"}

func (a Action) String() string {
	return actionStrs[a]
}

type Seat struct {
	Pos         int
	Stack       int
	Contributed int
	Acted       bool
	Folded      bool
	AllIn       bool
}

func (s *Seat) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (s *Seat) copy() *Seat {
	return &Seat{
		Pos:         s.Pos,
		Stack:       s.Stack,
		Contributed: s.Contributed,
		Acted:       s.Acted,
		Folded:      s.Folded,
		AllIn:       s.AllIn,
	}
}

type Pot struct {
	seats    []*Seat
	posToAct int
	cost     int
	bringIn  bool
	button   int
}

func Blinds(blinds []int) func(p *Pot) {
	// TODO catch invalid button
	return func(p *Pot) {
		p.posToAct = p.button
		if len(p.seats) == 2 {
			for _, blind := range blinds {
				p.contribute(p.SeatToAct(), blind, false)
				p.update()
			}
		} else {
			for _, blind := range blinds {
				p.update()
				p.contribute(p.SeatToAct(), blind, false)
			}
			p.update()
		}
	}
}

func BringIn(pos int, min int) func(p *Pot) {
	return func(p *Pot) {
		p.posToAct = pos
		p.cost = min
		p.bringIn = true
	}
}

func Ante(chips int) func(p *Pot) {
	return func(p *Pot) {
		for _, seat := range p.seats {
			p.contribute(seat, chips, false)
		}
	}
}

func New(stacks map[int]int, button int, opts ...func(*Pot)) *Pot {
	// TODO throw error for <= 1 seats
	seats := []*Seat{}
	for seat, stack := range stacks {
		seats = append(seats, &Seat{Pos: seat, Stack: stack})
	}
	sort.Slice(seats, func(i, j int) bool {
		return seats[i].Pos < seats[j].Pos
	})
	p := &Pot{
		button:   button,
		seats:    seats,
		posToAct: seats[0].Pos,
	}
	for _, f := range opts {
		f(p)
	}
	return p
}

func (p *Pot) Chips() int {
	total := 0
	for _, seat := range p.seats {
		total += seat.Contributed
	}
	return total
}

func (p *Pot) Cost() int {
	return p.cost
}

func (p *Pot) Seats() []*Seat {
	return append([]*Seat{}, p.seats...)
}

func (p *Pot) SeatToAct() *Seat {
	if p.posToAct == noPosToAct {
		return nil
	}
	return p.seats[p.posToAct]
}

func (p *Pot) PossibleActions() []Action {
	seat := p.SeatToAct()
	if seat == nil {
		return []Action{}
	}
	if p.bringIn {
		return []Action{Call, Raise, AllIn}
	}
	if p.cost == 0 {
		return []Action{Fold, Check, Bet, AllIn}
	}
	if p.cost >= seat.Stack {
		return []Action{Fold, Call}
	}
	return []Action{Fold, Call, Raise, AllIn}
}

func (p *Pot) Fold() error {
	if err := p.checkAction(Fold); err != nil {
		return err
	}
	seat := p.SeatToAct()
	seat.Acted = true
	seat.Folded = true
	p.update()
	return nil
}

func (p *Pot) Check() error {
	if err := p.checkAction(Check); err != nil {
		return err
	}
	seat := p.SeatToAct()
	seat.Acted = true
	p.update()
	return nil
}

func (p *Pot) Call() error {
	if err := p.checkAction(Call); err != nil {
		return err
	}
	seat := p.SeatToAct()
	p.contribute(seat, p.cost, true)
	p.update()
	return nil
}

func (p *Pot) Bet(chips int) error {
	if err := p.checkAction(Bet); err != nil {
		return err
	}
	for _, seat := range p.seats {
		seat.Acted = false
	}
	seat := p.SeatToAct()
	p.contribute(seat, chips, true)
	p.update()
	return nil
}

func (p *Pot) Raise(chips int) error {
	if err := p.checkAction(Raise); err != nil {
		return err
	}
	for _, seat := range p.seats {
		seat.Acted = false
	}
	seat := p.SeatToAct()
	p.contribute(seat, p.cost+chips, true)
	p.update()
	return nil
}

func (p *Pot) AllIn() error {
	if err := p.checkAction(AllIn); err != nil {
		return err
	}
	seat := p.SeatToAct()
	if includes(p.PossibleActions(), Raise) {
		return p.Raise(seat.Stack - p.cost)
	}
	return p.Bet(seat.Stack)
}

func (p *Pot) NextRound() {
	for _, seat := range p.seats {
		seat.Acted = false
	}
	p.posToAct = p.button
	p.update()
}

func (p *Pot) NextRoundWithPosition(pos int) {
	// TODO check if valid
	for _, seat := range p.seats {
		seat.Acted = false
	}
	p.posToAct = pos - 1
	p.update()
}

func (p *Pot) Uncontested() *Payout {
	var stillIn *Seat
	for _, seat := range p.seats {
		if seat.Folded == false && stillIn == nil {
			stillIn = seat
		} else if seat.Folded == false {
			return nil
		}
	}
	return &Payout{Pos: stillIn.Pos, Chips: p.Chips(), Share: WonUncontested}
}

// Share is the rights a winner has to the pot.
type Share int

const (
	WonUncontested Share = iota
	WonHigh
	WonLow
	SplitHigh
	SplitLow
)

var (
	shareStrs = []string{"WonUncontested", "WonHigh", "WonLow", "SplitHigh", "SplitLow"}
)

func (s Share) String() string {
	return shareStrs[s]
}

func (s Share) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *Share) UnmarshalText(text []byte) error {
	for i, str := range shareStrs {
		if string(text) == str {
			*s = Share(i)
			return nil
		}
	}
	return errors.New("pot: unrecognized share " + string(text))
}

// A Payout is a player's winning result from a showdown.
type Payout struct {
	Pos   int
	Chips int
	Share Share
}

func (p *Payout) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// Payout divides the pot among the winning high and low seats.
func (p *Pot) Payout(highs, lows [][]int) []*Payout {
	if payout := p.Uncontested(); payout != nil {
		return []*Payout{payout}
	}
	payouts := []*Payout{}
	for total, seats := range p.sidePots() {
		highSeats := p.findPayoutSeats(highs, seats)
		lowSeats := p.findPayoutSeats(lows, seats)
		splitTotal := total
		splitRemainder := 0
		if len(highSeats) > 0 && len(lowSeats) > 0 {
			splitTotal = total / 2
			splitRemainder = total % 2
		}
		payouts = append(payouts, p.divideTotal(highSeats, splitTotal+splitRemainder, WonHigh, SplitHigh)...)
		payouts = append(payouts, p.divideTotal(lowSeats, splitTotal, WonLow, SplitLow)...)
	}
	return payouts
}

type potJSON struct {
	Seats    []*Seat `json:"seats"`
	PosToAct int     `json:"posToAct"`
	Cost     int     `json:"cost"`
	BringIn  bool    `json:"bringIn"`
	Button   int     `json:"button"`
}

func (p *Pot) MarshalJSON() ([]byte, error) {
	m := &potJSON{
		Seats:    p.seats,
		PosToAct: p.posToAct,
		Cost:     p.cost,
		BringIn:  p.bringIn,
		Button:   p.button,
	}
	return json.Marshal(m)
}

func (p *Pot) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func (p *Pot) divideTotal(seats []*Seat, total int, singular, plural Share) []*Payout {
	num := len(seats)
	if num == 0 {
		return []*Payout{}
	}
	if num == 1 {
		po := &Payout{
			Pos:   seats[0].Pos,
			Chips: total,
			Share: singular,
		}
		return []*Payout{po}
	}
	base := total / num
	remainder := total % num
	max := -1
	for _, seat := range seats {
		if seat.Pos > max {
			max = seat.Pos
		}
	}
	cp := append([]*Seat{}, seats...)
	sort.Slice(cp, func(i, j int) bool {
		orderI := (cp[i].Pos - p.button) % max
		orderJ := (cp[j].Pos - p.button) % max
		return orderI < orderJ
	})
	payouts := []*Payout{}
	for i := 0; i < num; i++ {
		amount := base
		if i < remainder {
			amount++
		}
		po := &Payout{
			Pos:   cp[i].Pos,
			Chips: amount,
			Share: plural,
		}
		payouts = append(payouts, po)
	}
	return payouts
}

func (p *Pot) findPayoutSeats(rankings [][]int, seats []*Seat) []*Seat {
	for _, rank := range rankings {
		found := []*Seat{}
		for _, pos := range rank {
			for _, seat := range seats {
				if seat.Pos == pos {
					found = append(found, seat)
				}
			}
		}
		if len(found) > 0 {
			return found
		}
	}
	return []*Seat{}
}

func (p *Pot) checkAction(a Action) error {
	seat := p.SeatToAct()
	if seat == nil {
		return errors.New("pot: no actions are available")
	}
	possible := p.PossibleActions()
	if includes(possible, a) == false {
		return fmt.Errorf("pot: seat %d can't %s, available actions are %s", seat.Pos, a, possible)
	}
	return nil
}

func (p *Pot) update() {
	p.moveAction()
	p.setCost()
	p.bringIn = false
}

func (p *Pot) moveAction() {
	if p.posToAct == noPosToAct {
		return
	}
	if p.Uncontested() != nil {
		p.posToAct = noPosToAct
	}
	for i := 1; i < len(p.seats); i++ {
		a := (p.posToAct + i) % len(p.seats)
		if p.seats[a].Folded == false && p.seats[a].AllIn == false && p.seats[a].Acted == false {
			p.posToAct = a
			return
		}
	}
	p.posToAct = noPosToAct
}

func (p *Pot) setCost() {
	if p.posToAct == noPosToAct {
		p.cost = 0
		return
	}
	highest := 0
	for _, seat := range p.seats {
		if seat.Contributed > highest {
			highest = seat.Contributed
		}
	}
	p.cost = highest - p.SeatToAct().Contributed
}

func (p *Pot) contribute(seat *Seat, chips int, acted bool) {
	stack := seat.Stack
	amount := chips
	if stack <= chips {
		amount = stack
		seat.AllIn = true
	}
	seat.Contributed += amount
	seat.Stack -= amount
	seat.Acted = acted
}

func includes(actions []Action, include ...Action) bool {
	for _, a1 := range include {
		found := false
		for _, a2 := range actions {
			found = found || a1 == a2
		}
		if !found {
			return false
		}
	}
	return true
}

// sidePotAmounts finds the side pot totals seats are eligible for
func (p *Pot) sidePots() map[int][]*Seat {
	amounts := []int{}
	for _, seat := range p.seats {
		if seat.Contributed != 0 && seat.Folded == false {
			amounts = append(amounts, seat.Contributed)
		}
	}
	amounts = dedupe(amounts)
	sort.IntSlice(amounts).Sort()
	sidePots := map[int][]*Seat{}
	for i, a := range amounts {
		prev := 0
		if i != 0 {
			prev = amounts[i-1]
		}
		total := 0
		in := []*Seat{}
		for _, seat := range p.seats {
			if seat.Contributed >= a {
				in = append(in, seat)
				total += a - prev
			}
		}
		sidePots[total] = in
	}
	return sidePots
}

func dedupe(a []int) []int {
	m := map[int]bool{}
	for _, i := range a {
		m[i] = true
	}
	out := []int{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
