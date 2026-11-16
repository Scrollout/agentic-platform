Feature: greeting

  Rule: R-1 A greeting works
    @R-1 @E-1
    Scenario: greet passes
      Given I greet "ann"
      Then it works

  Rule: R-2 A greeting is bound
    @R-2 @E-2
    Scenario: greet is undefined
      Given nobody has defined this step
      Then it works
