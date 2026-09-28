Feature: Signing in without pop-ups
  The site uses a sign-in page, never a browser pop-up. Kris is the admin;
  anyone else enters as a guest with the entry phrase and can only look.

  Background:
    Given the admin is "kris" with password "admin-pw"
    And the guest entry phrase is "guest-phrase"

  Scenario: A signed-out visitor is sent to the sign-in page
    When a signed-out visitor opens the board
    Then they are redirected to the sign-in page
    And no browser sign-in pop-up is requested

  Scenario: The sign-in page offers a guest button
    When a signed-out visitor opens the sign-in page
    Then the page has a "Sign in as guest" button
    And the guest entry phrase form starts collapsed

  Scenario: Kris signs in as the admin
    When "kris" signs in with password "admin-pw"
    Then they are signed in as the admin

  Scenario: A guest enters with the entry phrase
    When a guest named "Toad" enters with phrase "guest-phrase"
    Then they are signed in as guest "toad"

  Scenario: A guest can leave the name blank
    When a guest named "" enters with phrase "guest-phrase"
    Then they are signed in as guest "guest"

  Scenario Outline: Wrong details are refused
    When "<name>" signs in with password "<password>"
    Then sign-in fails

    Examples:
      | name | password   |
      | kris | guest-phrase |
      | kris | wrong      |
      | toad | admin-pw   |

  Scenario: A guest can't claim the admin's name
    When a guest named "kris" enters with phrase "guest-phrase"
    Then sign-in fails

  Scenario: Signing out ends the session
    Given "kris" is signed in
    When they sign out
    Then opening the board redirects to the sign-in page
