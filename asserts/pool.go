// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package asserts

import (
	"errors"
	"fmt"

	"github.com/snapcore/snapd/asserts/internal"
)

// A Grouping identifies opaquely a grouping of assertions.
// Pool uses it to label the interesection between a set of groups.
type Grouping string

// A pool helps holding and tracking a set of assertions and their
// prerequisites as they need to be updated or resolved.  The
// assertions can be organized in groups. Failure can be tracked
// isolated to groups, conversely any error related to a single group
// alone will stop any work to resolve it. Independent assertions
// should not be grouped. Assertions and prerequisites that are part
// of more than one group are tracked properly only once.
//
// Typical usage involves specifying the initial assertions needing to
// be resolved or updated using AddUnresolved and AddToUpdate. At this
// point ToResolve can be called to get them organized in groupings
// ready for fetching. Fetched assertions can then be provided with
// Add or AddBatch. Because these can have prerequisites calling
// ToResolve and fetching needs to be repeated until ToResolve's
// result is empty.  Between any two ToResolve invocations but after
// any Add or AddBatch AddUnresolved/AddToUpdate can also be used
// again.
//
//                      V
//                      |
//        /-> AddUnresolved, AddToUpdate
//        |             |
//        |             V
//        |------> ToResolve -> empty? done
//        |             |
//        |             V
//        \ __________ Add
//
//
// If errors prevent from fulfilling assertions from a ToResolve,
// AddError and AddGroupingError can be used to report the errors so
// that they can be associated with groups.
//
// All the resolved assertions in a Pool from groups not in error can
// be committed to a destination database with CommitTo.
type Pool struct {
	groundDB RODatabase

	numbering map[string]uint16
	groupings *internal.Groupings

	unresolved    map[string]*unresolvedRec
	prerequisites map[string]*unresolvedRec

	bs        Backstore
	unchanged map[string]bool

	groups map[uint16]*groupRec

	curPhase poolPhase
}

// NewPool creates a new Pool, groundDB is used to resolve trusted and
// predefined assertions and to provide the current revision for
// assertions to update and their prerequisites. Up to n groups can be
// used to organize the assertions.
func NewPool(groundDB RODatabase, n int) *Pool {
	groupings, err := internal.NewGroupings(n)
	if err != nil {
		panic(fmt.Sprintf("NewPool: %v", err))
	}
	return &Pool{
		groundDB:      groundDB,
		numbering:     make(map[string]uint16),
		groupings:     groupings,
		unresolved:    make(map[string]*unresolvedRec),
		prerequisites: make(map[string]*unresolvedRec),
		bs:            NewMemoryBackstore(),
		unchanged:     make(map[string]bool),
		groups:        make(map[uint16]*groupRec),
	}
}

func (p *Pool) groupNum(group string) (gnum uint16, err error) {
	if gnum, ok := p.numbering[group]; ok {
		return gnum, nil
	}
	gnum = uint16(len(p.numbering))
	if err = p.groupings.WithinRange(gnum); err != nil {
		return 0, err
	}
	p.numbering[group] = gnum
	return gnum, nil
}

func (p *Pool) ensureGroup(group string) (gnum uint16, err error) {
	gnum, err = p.groupNum(group)
	if err != nil {
		return 0, err
	}
	if gRec := p.groups[gnum]; gRec == nil {
		p.groups[gnum] = &groupRec{
			name: group,
		}
	}
	return gnum, nil
}

// Singleton returns a grouping containing only the given group.
// It is useful mainly for tests and to drive Add are AddBatch when the
// server is pushing assertions (instead of the usual pull scenario).
func (p *Pool) Singleton(group string) (Grouping, error) {
	gnum, err := p.ensureGroup(group)
	if err != nil {
		return Grouping(""), nil
	}

	var grouping internal.Grouping
	p.groupings.AddTo(&grouping, gnum)
	return Grouping(p.groupings.Serialize(&grouping)), nil
}

// An unresolvedRec tracks a single unresolved assertion until it is
// resolved or there is an error doing so. The field 'grouping' will
// grow to contain all the groups requiring this assertion while it
// is unresolved.
type unresolvedRec struct {
	at       *AtRevision
	grouping internal.Grouping

	serializedLabel Grouping

	err error
}

func (u *unresolvedRec) exportTo(r map[Grouping][]*AtRevision, gr *internal.Groupings) {
	serLabel := Grouping(gr.Serialize(&u.grouping))
	// remember serialized label
	u.serializedLabel = serLabel
	r[serLabel] = append(r[serLabel], u.at)
}

func (u *unresolvedRec) merge(at *AtRevision, gnum uint16, gr *internal.Groupings) {
	gr.AddTo(&u.grouping, gnum)
	// assume we want to resolve/update wrt the highest revision
	if at.Revision > u.at.Revision {
		u.at.Revision = at.Revision
	}
}

// A groupRec keeps track of all the resolved assertions in a group
// or whether the group should be considered in error (err != nil).
type groupRec struct {
	name     string
	err      error
	resolved []Ref
}

func (gRec *groupRec) hasErr() bool {
	return gRec.err != nil
}

func (gRec *groupRec) setErr(e error) {
	if gRec.err == nil {
		gRec.err = e
	}
}

func (gRec *groupRec) markResolved(ref *Ref) (marked bool) {
	if gRec.hasErr() {
		return false
	}
	gRec.resolved = append(gRec.resolved, *ref)
	return true
}

// markResolved marks the assertion referenced by ref as resolved
// in all the groups in grouping, except those already in error.
func (p *Pool) markResolved(grouping *internal.Grouping, resolved *Ref) (marked bool) {
	p.groupings.Iter(grouping, func(gnum uint16) error {
		if p.groups[gnum].markResolved(resolved) {
			marked = true
		}
		return nil
	})
	return marked
}

// setErr marks all the groups in grouping as in error with error err
// except those already in error.
func (p *Pool) setErr(grouping *internal.Grouping, err error) {
	p.groupings.Iter(grouping, func(gnum uint16) error {
		p.groups[gnum].setErr(err)
		return nil
	})
}

func (p *Pool) isPredefined(ref *Ref) (bool, error) {
	_, err := ref.Resolve(p.groundDB.FindPredefined)
	if err == nil {
		return true, nil
	}
	if !IsNotFound(err) {
		return false, err
	}
	return false, nil
}

func (p *Pool) isResolved(ref *Ref) (bool, error) {
	if p.unchanged[ref.Unique()] {
		return true, nil
	}
	_, err := p.bs.Get(ref.Type, ref.PrimaryKey, ref.Type.MaxSupportedFormat())
	if err == nil {
		return true, nil
	}
	if !IsNotFound(err) {
		return false, err
	}
	return false, nil
}

func (p *Pool) curRevision(ref *Ref) (int, error) {
	a, err := ref.Resolve(p.groundDB.Find)
	if err != nil && !IsNotFound(err) {
		return 0, err
	}
	if err == nil {
		return a.Revision(), nil
	}
	return RevisionNotKnown, nil
}

type poolPhase int

const (
	poolPhaseAddUnresolved = iota
	poolPhaseAdd
)

func (p *Pool) phase(ph poolPhase) error {
	if ph == p.curPhase {
		return nil
	}
	if ph == poolPhaseAdd {
		return fmt.Errorf("internal error: cannot switch to Pool add phase without invoking ToResolve first")
	}
	// ph == poolPhaseAddUnresolved
	p.unresolvedBookkeeping()
	p.curPhase = poolPhaseAddUnresolved
	return nil
}

// AddUnresolved adds the assertion referenced by unresolved
// AtRevision to the Pool as unresolved and as required by the given group.
// Usually unresolved.Revision will have been set to RevisionNotKnown.
func (p *Pool) AddUnresolved(unresolved *AtRevision, group string) error {
	if err := p.phase(poolPhaseAddUnresolved); err != nil {
		return err
	}
	gnum, err := p.ensureGroup(group)
	if err != nil {
		return err
	}
	u := *unresolved
	ok, err := p.isPredefined(&u.Ref)
	if err != nil {
		return err
	}
	if ok {
		// predefined, nothing to do
		return nil
	}
	return p.addUnresolved(&u, gnum)
}

func (p *Pool) addUnresolved(unresolved *AtRevision, gnum uint16) error {
	ok, err := p.isResolved(&unresolved.Ref)
	if err != nil {
		return err
	}
	if ok {
		// We assume that either the resolving of
		// prerequisites for the already resolved assertion in
		// progress has succeeded or will. If that's not the
		// case we will fail at CommitTo time. We could
		// instead recurse into its prerequisites again but the
		// complexity isn't clearly worth it.
		// See TestParallelPartialResolutionFailure
		// Mark this as resolved in the group.
		p.groups[gnum].markResolved(&unresolved.Ref)
		return nil
	}
	uniq := unresolved.Ref.Unique()
	var u *unresolvedRec
	if u = p.unresolved[uniq]; u == nil {
		u = &unresolvedRec{
			at: unresolved,
		}
		p.unresolved[uniq] = u
	}
	u.merge(unresolved, gnum, p.groupings)
	return nil
}

// ToResolve returns all the currently unresolved assertions in the
// Pool, organized in opaque groupings based on which set of groups
// requires each of them.
// At the next ToResolve any unresolved assertion with not known
// revision that was not added via Add or AddBatch will result in all
// groups requiring it being in error with ErrUnresolved.
// Conversely, the remaining unresolved assertions originally added
// via AddToUpdate will be assumed to still be at their current
// revisions.
func (p *Pool) ToResolve() (map[Grouping][]*AtRevision, error) {
	if p.curPhase == poolPhaseAdd {
		p.unresolvedBookkeeping()
	} else {
		p.curPhase = poolPhaseAdd
	}
	r := make(map[Grouping][]*AtRevision)
	for _, u := range p.unresolved {
		if u.at.Revision == RevisionNotKnown {
			rev, err := p.curRevision(&u.at.Ref)
			if err != nil {
				return nil, err
			}
			if rev != RevisionNotKnown {
				u.at.Revision = rev
			}
		}
		u.exportTo(r, p.groupings)
	}
	return r, nil
}

func (p *Pool) addPrerequisite(pref *Ref, g *internal.Grouping) error {
	uniq := pref.Unique()
	u := p.unresolved[uniq]
	at := &AtRevision{
		Ref:      *pref,
		Revision: RevisionNotKnown,
	}
	if u == nil {
		u = p.prerequisites[uniq]
	}
	if u != nil {
		gr := p.groupings
		gr.Iter(g, func(gnum uint16) error {
			u.merge(at, gnum, gr)
			return nil
		})
		return nil
	}
	ok, err := p.isPredefined(pref)
	if err != nil {
		return err
	}
	if ok {
		// nothing to do
		return nil
	}
	ok, err = p.isResolved(pref)
	if err != nil {
		return err
	}
	if ok {
		// nothing to do, it is anyway implied
		return nil
	}
	p.prerequisites[uniq] = &unresolvedRec{
		at:       at,
		grouping: g.Copy(),
	}
	return nil
}

func (p *Pool) add(a Assertion, g *internal.Grouping) error {
	if err := p.bs.Put(a.Type(), a); err != nil {
		if revErr, ok := err.(*RevisionError); ok {
			if revErr.Current >= a.Revision() {
				// we already got something more recent
				return nil
			}
		}

		return err
	}
	for _, pref := range a.Prerequisites() {
		if err := p.addPrerequisite(pref, g); err != nil {
			return err
		}
	}
	keyRef := &Ref{
		Type:       AccountKeyType,
		PrimaryKey: []string{a.SignKeyID()},
	}
	if err := p.addPrerequisite(keyRef, g); err != nil {
		return err
	}
	return nil
}

func (p *Pool) resolveWith(unresolved map[string]*unresolvedRec, uniq string, u *unresolvedRec, a Assertion, extrag *internal.Grouping) (ok bool, err error) {
	if a.Revision() > u.at.Revision {
		if extrag == nil {
			extrag = &u.grouping
		} else {
			p.groupings.Iter(&u.grouping, func(gnum uint16) error {
				p.groupings.AddTo(extrag, gnum)
				return nil
			})
		}
		ref := a.Ref()
		if p.markResolved(extrag, ref) {
			// remove from tracking -
			// remove u from unresolved only if the assertion
			// is added to the resolved backstore;
			// otherwise it might resurface as unresolved;
			// it will be ultimately handled in
			// unresolvedBookkeeping if it stays around
			delete(unresolved, uniq)
			if err := p.add(a, extrag); err != nil {
				p.setErr(extrag, err)
				return false, err
			}
		}
	}
	return true, nil
}

// Add adds the given assertion associated with the given grouping to the
// Pool as resolved in all the groups requiring it.
// Any not already resolved prerequisites of the assertion will
// be implicitly added as unresolved and required by all of those groups.
// The grouping will usually have been associated with the assertion
// in a ToResolve's result. Otherwise the union of all groups
// requiring the assertion plus the groups in grouping will be considered.
// The latter is mostly relevant in scenarios where the server is pushing
// assertions.
// If an error is returned it refers to an immediate or local error.
// Errors related to the assertions are associated with the relevant groups
// and can be retrieved with Err, in which case ok is set to false.
func (p *Pool) Add(a Assertion, grouping Grouping) (ok bool, err error) {
	if err := p.phase(poolPhaseAdd); err != nil {
		return false, err
	}

	if !a.SupportedFormat() {
		e := &UnsupportedFormatError{Ref: a.Ref(), Format: a.Format()}
		p.AddGroupingError(e, grouping)
		return false, nil
	}

	return p.addToGrouping(a, grouping, p.groupings.Deserialize)
}

func (p *Pool) addToGrouping(a Assertion, grouping Grouping, deserializeGrouping func(string) (*internal.Grouping, error)) (ok bool, err error) {
	uniq := a.Ref().Unique()
	var u *unresolvedRec
	var extrag *internal.Grouping
	var unresolved map[string]*unresolvedRec
	if u = p.unresolved[uniq]; u != nil {
		unresolved = p.unresolved
	} else if u = p.prerequisites[uniq]; u != nil {
		unresolved = p.prerequisites
	} else {
		ok, err := p.isPredefined(a.Ref())
		if err != nil {
			return false, err
		}
		if ok {
			// nothing to do
			return true, nil
		}
		// a is not tracked as unresolved in any way so far,
		// this is an atypical scenario where something gets
		// pushed but we still want to add it to the resolved
		// lists of the relevant groups; in case it is
		// actually already resolved most of resolveWith below will
		// be a nop
		u = &unresolvedRec{
			at: a.At(),
		}
		u.at.Revision = RevisionNotKnown
	}

	if u.serializedLabel != grouping {
		var err error
		extrag, err = deserializeGrouping(string(grouping))
		if err != nil {
			return false, err
		}
	}

	return p.resolveWith(unresolved, uniq, u, a, extrag)
}

// AddBatch adds all the assertions in the Batch to the Pool,
// associated with the given grouping and as resolved in all the
// groups requiring them. It is equivalent to using Add on each of them.
// If an error is returned it refers to an immediate or local error.
// Errors related to the assertions are associated with the relevant groups
// and can be retrieved with Err, in which case ok set to false.
func (p *Pool) AddBatch(b *Batch, grouping Grouping) (ok bool, err error) {
	if err := p.phase(poolPhaseAdd); err != nil {
		return false, err
	}

	// b dealt with unsupported formats already

	// deserialize grouping if needed only once
	var cachedGrouping *internal.Grouping
	deser := func(_ string) (*internal.Grouping, error) {
		if cachedGrouping != nil {
			// do a copy as addToGrouping and resolveWith
			// might add to their input
			g := cachedGrouping.Copy()
			return &g, nil
		}
		var err error
		cachedGrouping, err = p.groupings.Deserialize(string(grouping))
		return cachedGrouping, err
	}

	inError := false
	for _, a := range b.added {
		ok, err := p.addToGrouping(a, grouping, deser)
		if err != nil {
			return false, err
		}
		if !ok {
			inError = true
		}
	}

	return !inError, nil
}

var (
	ErrUnresolved       = errors.New("unresolved assertion")
	ErrUnknownPoolGroup = errors.New("unknown pool group")
)

// unresolvedBookkeeping processes any left over unresolved assertions
// since the last ToResolve invocation and intervening calls to Add/AddBatch,
//  * they were either marked as in error which will be propagated
//    to all groups requiring them
//  * simply unresolved, which will be propagated to groups requiring them
//    as ErrUnresolved
//  * unchanged (update case)
// unresolvedBookkeeping will also promote any recorded prerequisites
// into actively unresolved, as long as not all the groups requiring them
// are in error.
func (p *Pool) unresolvedBookkeeping() {
	// any left over unresolved are either:
	//  * in error
	//  * unchanged
	//  * or unresolved
	for uniq, u := range p.unresolved {
		e := u.err
		if e == nil {
			if u.at.Revision == RevisionNotKnown {
				e = ErrUnresolved
			} else {
				// unchanged
				p.unchanged[uniq] = true
			}
		}
		if e != nil {
			p.setErr(&u.grouping, e)
		}
		delete(p.unresolved, uniq)
	}

	// prerequisites will become the new unresolved but drop them
	// if all their groups are in error
	for uniq, prereq := range p.prerequisites {
		useful := false
		p.groupings.Iter(&prereq.grouping, func(gnum uint16) error {
			if !p.groups[gnum].hasErr() {
				useful = true
			}
			return nil
		})
		if !useful {
			delete(p.prerequisites, uniq)
			continue
		}
	}

	// prerequisites become the new unresolved, the emptied
	// unresolved is used for prerequisites in the next round
	p.unresolved, p.prerequisites = p.prerequisites, p.unresolved
}

// Err returns the error for group if group is in error, nil otherwise.
func (p *Pool) Err(group string) error {
	gnum, err := p.groupNum(group)
	if err != nil {
		return err
	}
	gRec := p.groups[gnum]
	if gRec == nil {
		return ErrUnknownPoolGroup
	}
	return gRec.err
}

// Errors returns a mapping of groups in error to their errors.
func (p *Pool) Errors() map[string]error {
	res := make(map[string]error)
	for _, gRec := range p.groups {
		if err := gRec.err; err != nil {
			res[gRec.name] = err
		}
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

// AddError associates error e with the unresolved assertion.
// The error will be propagated to all the affected groups at
// the next ToResolve.
func (p *Pool) AddError(e error, ref *Ref) error {
	if err := p.phase(poolPhaseAdd); err != nil {
		return err
	}
	uniq := ref.Unique()
	if u := p.unresolved[uniq]; u != nil && u.err == nil {
		u.err = e
	}
	return nil
}

// AddGroupingError puts all the groups of grouping in error, with error e.
func (p *Pool) AddGroupingError(e error, grouping Grouping) error {
	if err := p.phase(poolPhaseAdd); err != nil {
		return err
	}

	g, err := p.groupings.Deserialize(string(grouping))
	if err != nil {
		return err
	}

	p.setErr(g, e)
	return nil
}

// AddToUpdate adds the assertion referenced by toUpdate and all its
// prerequisites to the Pool as unresolved and as required by the
// given group. It is assumed that the assertion is currently in the
// ground database of the Pool, otherwise this will error.
// The current revisions of the assertion and its prerequisites will
// be recorded and only higher revisions will then resolve them,
// otherwise if ultimately unresolved they will be assumed to still be
// at their current ones.
func (p *Pool) AddToUpdate(toUpdate *Ref, group string) error {
	if err := p.phase(poolPhaseAddUnresolved); err != nil {
		return err
	}
	gnum, err := p.ensureGroup(group)
	if err != nil {
		return err
	}
	retrieve := func(ref *Ref) (Assertion, error) {
		return ref.Resolve(p.groundDB.Find)
	}
	add := func(a Assertion) error {
		return p.addUnresolved(a.At(), gnum)
	}
	f := NewFetcher(p.groundDB, retrieve, add)
	if err := f.Fetch(toUpdate); err != nil {
		return err
	}
	return nil
}

// CommitTo adds the assertions from groups without errors to the
// given assertion database. Commit errors can be retrieved via Err
// per group. An error is returned directly only if CommitTo is called
// with possible pending unresolved assertions.
func (p *Pool) CommitTo(db *Database) error {
	if p.curPhase == poolPhaseAddUnresolved {
		return fmt.Errorf("internal error: cannot commit Pool during add unresolved phase")
	}
	p.unresolvedBookkeeping()

	retrieve := func(ref *Ref) (Assertion, error) {
		a, err := p.bs.Get(ref.Type, ref.PrimaryKey, ref.Type.MaxSupportedFormat())
		if IsNotFound(err) {
			// fallback to pre-existing assertions
			a, err = ref.Resolve(db.Find)
		}
		if err != nil {
			return nil, resolveError("cannot resolve prerequisite assertion: %s", ref, err)
		}
		return a, nil
	}
	save := func(a Assertion) error {
		err := db.Add(a)
		if IsUnaccceptedUpdate(err) {
			// unsupported format case is handled before.
			// be idempotent, db has already the same or
			// newer.
			return nil
		}
		return err
	}

NextGroup:
	for _, gRec := range p.groups {
		if gRec.hasErr() {
			// already in error, ignore
			continue
		}
		// TODO: try to reuse fetcher
		f := NewFetcher(db, retrieve, save)
		for i := range gRec.resolved {
			if err := f.Fetch(&gRec.resolved[i]); err != nil {
				gRec.setErr(err)
				continue NextGroup
			}
		}
	}

	return nil
}

// ClearGroups clears the pool in terms of information associated with groups
// while preserving information about already resolved or unchanged assertions.
// It is useful for reusing a pool once the maximum of usable groups
// that was set with NewPool has been exhausted. Group errors must be
// queried before calling it otherwise they are lost. It is an error
// to call it when there are still pending unresolved assertions in
// the pool.
func (p *Pool) ClearGroups() error {
	if len(p.unresolved) != 0 || len(p.prerequisites) != 0 {
		return fmt.Errorf("internal error: trying to clear groups of asserts.Pool with pending unresolved or prerequisites")
	}

	p.numbering = make(map[string]uint16)
	// use a fresh Groupings as well so that max group tracking starts
	// from scratch.
	// NewGroupings cannot fail on a value accepted by it previously
	p.groupings, _ = internal.NewGroupings(p.groupings.N())
	p.groups = make(map[uint16]*groupRec)
	p.curPhase = poolPhaseAdd
	return nil
}
