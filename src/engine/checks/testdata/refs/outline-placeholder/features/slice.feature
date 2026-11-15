Feature: greeting

  Rule: R-1 Greetings
    @R-1 @E-1
    Scenario Outline: greet <name>
      Given I greet "<name>"
      Then it works

      Examples:
        | name |
        | ann  |
        | bob  |
