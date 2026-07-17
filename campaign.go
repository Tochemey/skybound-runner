package main

// StageDef describes one playable campaign stage.
type StageDef struct {
	World     int
	Stage     int
	Name      string
	Theme     StageTheme
	TimeLimit int
	FlagCol   int
	Build     func() Level
}

// campaign is the ordered, 10-stage run: three accessible Sunny Fields
// stages, three Pipe Works stages, and four Ember Ruins stages.
var campaign = []StageDef{
	{World: 1, Stage: 1, Name: "Meadow Start", Theme: ThemeSunnyFields, TimeLimit: 300, FlagCol: 88, Build: stageOne},
	{World: 1, Stage: 2, Name: "Bramble Path", Theme: ThemeSunnyFields, TimeLimit: 300, FlagCol: 90, Build: sunnyPath},
	{World: 1, Stage: 3, Name: "Hilltop Dash", Theme: ThemeSunnyFields, TimeLimit: 320, FlagCol: 94, Build: sunnyHills},
	{World: 2, Stage: 1, Name: "Steam Crossing", Theme: ThemePipeWorks, TimeLimit: 300, FlagCol: 88, Build: stageTwo},
	{World: 2, Stage: 2, Name: "Valve Vault", Theme: ThemePipeWorks, TimeLimit: 320, FlagCol: 92, Build: pipeVault},
	{World: 2, Stage: 3, Name: "High Pressure", Theme: ThemePipeWorks, TimeLimit: 330, FlagCol: 96, Build: pipeRun},
	{World: 3, Stage: 1, Name: "Ashen Gate", Theme: ThemeEmberRuins, TimeLimit: 320, FlagCol: 88, Build: stageThree},
	{World: 3, Stage: 2, Name: "Cinder Steps", Theme: ThemeEmberRuins, TimeLimit: 330, FlagCol: 92, Build: emberSteps},
	{World: 3, Stage: 3, Name: "Obsidian Span", Theme: ThemeEmberRuins, TimeLimit: 340, FlagCol: 94, Build: emberSpan},
	{World: 3, Stage: 4, Name: "Crown of Embers", Theme: ThemeEmberRuins, TimeLimit: 360, FlagCol: 96, Build: emberCrown},
}

// stageDefinition returns the selected stage definition. The caller must only
// request valid campaign indices; invalid indices resolve to the first stage
// to keep a malformed client request from panicking an actor.
func stageDefinition(index int) StageDef {
	if index < 0 || index >= len(campaign) {
		return campaign[0]
	}
	return campaign[index]
}

// buildStage builds a stage and applies its shared campaign metadata.
func buildStage(index int) (Level, StageDef) {
	def := stageDefinition(index)
	level := def.Build()
	level.Theme = def.Theme
	level.FlagX = float64(def.FlagCol)
	placeFlagAt(&level.Tiles, def.FlagCol)
	return level, def
}
