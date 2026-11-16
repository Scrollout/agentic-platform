Feature: greeting

  Rule: R-1 A greeting works
    @R-1 @E-1
    Scenario: greet passes
      Given I greet "ann"
      Then it works
