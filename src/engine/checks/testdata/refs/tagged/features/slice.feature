Feature: greeting

  Rule: R-1 A greeting works
    @R-1 @E-1
    Scenario: greet passes
      Given I greet "ann"
      Then it works

    @R-1 @E-2
    Scenario: greet fails
      Given I do the thing
      Then it works

  Rule: R-2 Many greetings work
    @R-2 @E-3
    Scenario Outline: greet many
      Given I greet "<name>"
      Then <outcome>

      Examples:
        | name | outcome   |
        | ann  | it works  |
        | bob  | it breaks |

  Rule: R-3 An untagged scenario
    Scenario: untagged and failing
      Given I do the thing
      Then it works
