@ui
Feature: The draft board page
  One column, no modals: every action expands in place next to where it
  was clicked, and every expanded panel can be collapsed again.
  (Checked in a real browser at 375, 768 and 1280 px wide; not part of
  `go test`.)

  Background:
    Given Kris is signed in on the board

  Scenario: The page is one column
    Then Best buys, the player list, my team, recent picks and the draft board are stacked full width
    And the draft board spans the full page width

  Scenario: Ranking weights are collapsible
    Then "Ranking weights" is collapsed
    When Kris expands "Ranking weights"
    Then the weight sliders are shown
    When Kris collapses "Ranking weights"
    Then the weight sliders are hidden

  Scenario: Ticking a player asks who drafted him, in the row
    When Kris ticks "Connor McDavid"
    Then a "Drafted by" pool-team choice appears in that row
    When Kris chooses "Sniffer" and saves
    Then the row shows "Sniffer"
    And the player leaves the list because drafted players are hidden

  Scenario: The drafted-by choice can be skipped
    When Kris ticks "Connor McDavid"
    And Kris skips choosing who drafted him
    Then the player leaves the list

  Scenario: Clicking a name opens an editor under the row, and closes it again
    When Kris clicks "Mark Stone"
    Then an editor opens directly under his row
    When Kris clicks "Mark Stone" again
    Then the editor closes

  Scenario: Live updates don't wipe what Kris is typing
    Given Kris is typing in a player's editor
    When another pick arrives from the server
    Then what Kris typed is still there

  Scenario: Destructive actions confirm in place
    When Kris presses "Clear all picks"
    Then an inline "Yes" and "Cancel" appear next to it
    And no browser pop-up opens

  Scenario: There are no modals anywhere
    Then the page has no dialog elements
    And nothing calls confirm, prompt or alert
