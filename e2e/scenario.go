package e2e

import "math/rand"

// Encapsulates single scenario which holds array of actions being executed by this scenario
type Scenario struct {
	actions []Action
}

// Initialize a scenario
func newScenario() *Scenario {
	return &Scenario{
		actions: make([]Action, 0),
	}
}

// Add provided action into the scenario
func (s *Scenario) addAction(action Action) *Scenario {
	s.actions = append(s.actions, action)
	return s
}

// Execute all the actions from the scenario
func (s *Scenario) Execute(cluster *Cluster) {
	for _, action := range s.actions {
		action.Apply(cluster)
	}
}

// Revert all the actions from the scenario
func (s *Scenario) CleanUp(cluster *Cluster) {
	for _, action := range s.actions {
		action.Revert(cluster)
	}
}

// Encapsulates logic for scenarios generation
type ScenarioGenerator struct {
	scenariosCount          uint              // scenarios count
	actionsPerScenarioCount uint              // number of actions per each scenarios
	actionsRepo             *actionRepository // reference to the repository which contains all the pre-defined actions
}

func NewScenarioGenerator(scenariosCount, actionsPerScenarioCount uint) *ScenarioGenerator {
	return &ScenarioGenerator{
		scenariosCount:          scenariosCount,
		actionsPerScenarioCount: actionsPerScenarioCount,
		actionsRepo:             newActionRepository(),
	}
}

// Generate scenarioCount array of scenarios. Each scenario will contain actionsPerScenarioCount.
// Actions are randomly picked from the actionsRepository, which holds all available pre-defined actions.
func (g *ScenarioGenerator) GenerateScenarios() []*Scenario {
	scenarios := make([]*Scenario, g.scenariosCount)
	for i := 0; i < int(g.scenariosCount); i++ {
		currentScenario := newScenario()
		// TODO: Randomize how many actions will be added per scenario (but not less than 1 or 2 maybe)?
		// actionsCount := int(math.Max(2, float64(rand.Intn(int(g.actionsPerScenarioCount)))))
		for j := 0; j < int(g.actionsPerScenarioCount); j++ {
			actionIndex := rand.Intn(len(g.actionsRepo.actions))
			currentScenario.addAction(g.actionsRepo.actions[actionIndex])
		}
		scenarios[i] = currentScenario
	}
	return scenarios
}
