@ui
Feature: Signing in and the guest view, in the browser
  Checked in a real browser by ui-tests/ at 375, 768 and 1280 px.

  Scenario: The sign-in page, not a pop-up
    When a visitor opens the site
    Then they see the sign-in page
    And no browser pop-up opens

  Scenario: The guest form folds open and closed
    Given a visitor is on the sign-in page
    When they press "Sign in as guest"
    Then the entry phrase field is shown
    When they press Cancel
    Then the entry phrase field is hidden

  Scenario: Kris signs in and out
    Given a visitor is on the sign-in page
    When they sign in as Kris
    Then they see the board as "kris"
    When they press "Sign out"
    Then they see the sign-in page

  Scenario: A guest gets a read-only board with search
    Given a visitor is on the sign-in page
    When they enter as a guest named "Toad"
    Then they see the board as "toad (guest)"
    And there is no Best buys, Drafted column, Value column, Setup tab or ranking weights
    When they type "vgk" in the search box
    Then every player listed plays for "VGK"

  Scenario: Wrong details stay on the sign-in page
    Given a visitor is on the sign-in page
    When they sign in as "kris" with password "nope"
    Then they see "That didn't match" on the sign-in page
