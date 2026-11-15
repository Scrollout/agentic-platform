Feature: greeting

  Rule: R-1 First
    @R-1 @E-1
    Scenario: greet
      Given I greet "ann"
      Then it works

  Rule: R-2 Second
    @R-2 @E-2
    Scenario: greet
      Given I do the thing
      Then it works
