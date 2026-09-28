Feature: Tracking the auction
  Kris ticks players off as they're bought. Who bought them and the price
  are optional and can be filled in right after ticking, or later.

  Background:
    Given the pool teams are "Toad, Sniffer, Kris"
    And Kris is "Kris"
    And each team has $100 for 7 players with a $4 minimum bid
    And the player list is:
      | Name        | Team |
      | Mark Stone  | VGK  |
      | Jack Eichel | VGK  |
      | Connor Test | EDM  |

  Scenario: Ticking a player marks him drafted without a buyer
    When Kris marks "Mark Stone" as drafted
    Then "Mark Stone" is drafted by nobody recorded

  Scenario: Choosing who drafted a ticked player
    Given Kris marked "Mark Stone" as drafted
    When Kris sets "Mark Stone" drafted by "Sniffer" for $12
    Then "Mark Stone" is drafted by "Sniffer" for $12

  Scenario: Choosing who drafted without the price
    Given Kris marked "Mark Stone" as drafted
    When Kris sets "Mark Stone" drafted by "Toad" for $0
    Then "Mark Stone" is drafted by "Toad" for $0

  Scenario: Recording my own buy
    When Kris records "Jack Eichel" as bought by "Kris" for $20
    Then "Kris" has $80 left and can bid at most $60

  Scenario: Bids below the minimum are refused
    When Kris records "Jack Eichel" as bought by "Toad" for $3
    Then the request is refused with "at least $4"

  Scenario: Bids above what a team can afford are refused
    When Kris records "Jack Eichel" as bought by "Toad" for $77
    Then the request is refused with "at most $76"

  Scenario: Unticking puts a player back
    Given Kris marked "Mark Stone" as drafted
    When Kris unticks "Mark Stone"
    Then "Mark Stone" is available

  Scenario: Practice picks are cleared for the real draft
    Given Kris marked "Mark Stone" as drafted
    And Kris recorded "Jack Eichel" as bought by "Toad" for $30
    When Kris clears all picks
    Then no players are drafted
    And "Toad" has $100 left

  Scenario: Reordering pool teams keeps buys with their team
    Given Kris recorded "Jack Eichel" as bought by "Toad" for $30
    When Kris reorders the pool teams to "Kris, Toad, Sniffer"
    Then "Jack Eichel" is drafted by "Toad" for $30
