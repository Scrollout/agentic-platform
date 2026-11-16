Feature: greeting

  Rule: R-1 One
    @R-1 @E-1 @run
    Scenario: first runs and passes
      Given it works

  Rule: R-2 Two
    @R-2 @E-2
    Scenario: second is filtered out
      Given I do the thing

  Rule: R-3 Three
    @R-3 @E-3 @run
    Scenario: third runs and fails
      Given I do the thing
