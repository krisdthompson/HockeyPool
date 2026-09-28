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

  Scenario: The first column is headed Drafted
    Then the first column of the player list and of Best buys is headed "Drafted"

  Scenario: Ticking Drafted opens the details below the player
    When Kris ticks Drafted on "Connor McDavid"
    Then a details row opens directly below his row
    And it has an Owner choice of pool teams, an Amount and a Dismiss button
    When Kris chooses owner "Sniffer"
    Then the owner is saved without pressing a save button
    When Kris enters an amount of $58
    Then the amount is saved without pressing a save button
    When Kris presses Dismiss
    Then the details row closes
    And the player leaves the list because drafted players are hidden

  Scenario: Ticking doesn't move anything above the details
    When Kris ticks Drafted on a player partway down the list
    Then that player's row stays exactly where it was on screen
    And the details fold open below it

  Scenario: Unticking while the details are open undoes the tick
    When Kris ticks Drafted on "Connor McDavid"
    And Kris unticks him before dismissing the details
    Then the details close without asking
    And "Connor McDavid" is not drafted and is still in the list

  Scenario: Dismissing keeps him drafted without an owner
    When Kris ticks Drafted on "Connor McDavid"
    And Kris presses Dismiss
    Then the details row closes
    And "Connor McDavid" is drafted with no owner recorded

  Scenario: A drafted player's owner and amount can be corrected
    Given "Connor McDavid" was drafted by "Sniffer" for $58
    When Kris presses the pencil next to him
    Then the details row opens below him showing "Sniffer" and $58
    When Kris changes the owner to "Toad"
    Then the change is saved automatically

  Scenario: Editing a drafted player from the draft board
    Given "Connor McDavid" was drafted by "Sniffer" for $58
    When Kris clicks him on the draft board
    Then the details open under the draft board showing "Sniffer" and $58
    When Kris changes the amount to $60
    Then the change is saved automatically

  Scenario: Undrafting by unticking asks first
    Given "Connor McDavid" is drafted
    When Kris unticks Drafted on "Connor McDavid"
    Then he stays drafted and "Undraft Connor McDavid?" appears below his row
    And his row stays in the list while the question is showing
    When Kris presses Cancel
    Then he is still drafted
    When Kris unticks him again and presses "Yes, undraft"
    Then he is back on the board

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
