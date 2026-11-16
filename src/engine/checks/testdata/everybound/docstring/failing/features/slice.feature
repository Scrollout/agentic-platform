Feature: greeting

  # Scenario: a comment that is not a scenario
  @run
  Scenario: greet by name
    Given the text:
      """
      Scenario: inside a docstring
      Scenario Outline: also inside
      """
    Then it works

  Scenario: greet nobody
    Given I do the thing
    Then it works

