Feature: greeting

  @run
  Scenario Outline: greet many
    Given I greet "<name>"
    Then it works

    @run
    Examples:
      | name |
      | ann  |
      | bob  |

    @skip
    Examples:
      | name |
      | cy   |

  @run
  Scenario: greet nobody
    Given I do the thing
    Then it works

