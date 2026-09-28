Feature: Guests browse but only Kris gets recommendations
  Kris's flags and reasons are his strategy. Guests can search and read
  the player list but never receive those, and can't change the draft.

  Background:
    Given the admin is "kris" with password "admin-pw"
    And the guest entry phrase is "guest-phrase"
    And the player list is:
      | Name        | Team | Tag    | Why               |
      | Mark Stone  | VGK  | target | always on my team |
      | Connor Test | EDM  |        |                   |

  Scenario: Guests never receive Kris's flags or reasons
    Given a guest named "toad" is signed in
    When they load the draft
    Then the response does not contain "always on my team"
    And no player is flagged

  Scenario: Kris sees his own flags and reasons
    Given "kris" is signed in
    When they load the draft
    Then "Mark Stone" is flagged "target"

  Scenario: Guests can't tick players off
    Given a guest named "toad" is signed in
    When they mark "Connor Test" as drafted
    Then the request is refused as not allowed
