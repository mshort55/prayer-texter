package prayertexter

type StateTracker struct {
	Key    string
	States []State
}

type State struct {
	Error     string
	Message   TextMessage
	ID        string
	Stage     string
	Status    string
	TimeStart string
}

const (
	stateTrackerAttribute = "Key"
	stateTrackerKey       = "StateTracker"
	stateTrackerTable     = "General"
)

func (st *StateTracker) get(clnt DDBConnecter) error {
	sttrackr, err := getDdbObject[StateTracker](clnt, stateTrackerAttribute, stateTrackerKey, stateTrackerTable)
	if err != nil {
		return err
	}

	// this is important so that the original Member object doesn't get reset to all empty struct
	// values if the Member does not exist in ddb
	if sttrackr.Key != "" {
		*st = *sttrackr
	}

	return nil
}

func (st *StateTracker) put(clnt DDBConnecter) error {
	st.Key = stateTrackerKey
	return putDdbObject(clnt, string(stateTrackerTable), st)
}

func (s *State) update(clnt DDBConnecter, remove bool) error {
	st := StateTracker{}
	if err := st.get(clnt); err != nil {
		return err
	}

	states := &st.States
	for _, state := range st.States {
		if state.ID == s.ID {
			removeItem(states, state)
		}
	}

	if !remove {
		st.States = append(st.States, *s)
	}

	if err := st.put(clnt); err != nil {
		return err
	}

	return nil
}
